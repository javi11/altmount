package vfs

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sync/singleflight"
)

// noopFetchGroup is used when no downloader is present (standalone CachedFile).
var noopFetchGroup singleflight.Group

var fetchBufPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 256*1024) // 256KB default
		return &buf
	},
}

// FileOpener is the interface for opening files from the underlying filesystem.
// This decouples CachedFile from NzbFilesystem to allow testing.
type FileOpener interface {
	Open(ctx context.Context, name string) (afero.File, error)
}

// CachedFile wraps a CacheItem and provides ReadAt with automatic cache-miss fetching.
// Thread-safe and supports concurrent random access reads.
type CachedFile struct {
	item       *CacheItem
	opener     FileOpener
	path       string
	size       int64
	chunkSize  int64
	logger     *slog.Logger
	downloader *Downloader
	closed     atomic.Bool

	// fetchGroup deduplicates concurrent fetches for the same chunk range.
	// Shared with the Downloader so that sync-path and prefetch-path fetches
	// for the same range are collapsed into a single download.
	fetchGroup *singleflight.Group
}

// NewCachedFile creates a new CachedFile wrapping a CacheItem.
func NewCachedFile(
	item *CacheItem,
	opener FileOpener,
	path string,
	size int64,
	chunkSize int64,
	logger *slog.Logger,
	downloader *Downloader,
) (*CachedFile, error) {
	if err := item.Open(); err != nil {
		return nil, fmt.Errorf("open cache item: %w", err)
	}

	// Share the fetch dedup group with the downloader so that sync-path
	// and prefetch-path fetches for the same chunk are collapsed.
	var fg *singleflight.Group
	if downloader != nil {
		fg = downloader.FetchGroup()
	} else {
		fg = &noopFetchGroup
	}

	return &CachedFile{
		item:       item,
		opener:     opener,
		path:       path,
		size:       size,
		chunkSize:  chunkSize,
		logger:     logger,
		downloader: downloader,
		fetchGroup: fg,
	}, nil
}

// ReadAt implements random-access reads with cache-miss fetching.
// On cache miss, fetches aligned chunks from the backend and caches them.
func (cf *CachedFile) ReadAt(p []byte, off int64) (int, error) {
	if cf.closed.Load() {
		return 0, fmt.Errorf("file is closed")
	}

	if off >= cf.size {
		return 0, io.EOF
	}

	// Clamp read to file size
	readEnd := off + int64(len(p))
	if readEnd > cf.size {
		readEnd = cf.size
		p = p[:readEnd-off]
	}

	// Try reading from cache first
	n, ok := cf.item.ReadAt(p, off)
	if ok {
		if cf.downloader != nil {
			cf.downloader.RecordAccess(off)
		}
		return n, nil
	}

	// Cache miss — fetch from backend
	if err := cf.fetchRange(off, readEnd); err != nil {
		return 0, fmt.Errorf("fetch range: %w", err)
	}

	// Read from cache after fetch
	n, ok = cf.item.ReadAt(p, off)
	if !ok {
		return 0, fmt.Errorf("data not in cache after fetch")
	}

	// Notify downloader about the access for read-ahead
	if cf.downloader != nil {
		cf.downloader.RecordAccess(off)
	}

	return n, nil
}

// fetchRange fetches data from the backend and writes it to cache.
// Fetches aligned chunks to maximize cache reuse.
// Uses singleflight to deduplicate concurrent fetches for the same range
// while allowing fetches for different ranges to proceed in parallel.
func (cf *CachedFile) fetchRange(start, end int64) error {
	// Align to chunk boundaries
	alignedStart := (start / cf.chunkSize) * cf.chunkSize
	alignedEnd := ((end + cf.chunkSize - 1) / cf.chunkSize) * cf.chunkSize
	if alignedEnd > cf.size {
		alignedEnd = cf.size
	}

	// Quick check — avoid singleflight overhead when already cached
	missing := cf.item.MissingRanges(alignedStart, alignedEnd)
	if len(missing) == 0 {
		return nil
	}

	// Fetch each missing range via singleflight:
	// - Different chunks proceed in parallel (different keys)
	// - Same chunk from concurrent readers is deduplicated (same key)
	for _, r := range missing {
		key := fmt.Sprintf("%d-%d", r.Start, r.End)
		_, err, _ := cf.fetchGroup.Do(key, func() (interface{}, error) {
			return nil, cf.fetchAndCache(r.Start, r.End)
		})
		if err != nil {
			return err
		}
	}

	return nil
}

// fetchAndCache opens a backend file and reads the range using ReadAt, writing to cache.
// ReadAt creates a bounded reader covering only the requested range, avoiding
// the unbounded offset→EOF readers that Seek+Read would create.
func (cf *CachedFile) fetchAndCache(start, end int64) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	file, err := cf.opener.Open(ctx, cf.path)
	if err != nil {
		return fmt.Errorf("open backend file: %w", err)
	}
	defer file.Close()

	rangeLen := end - start

	// For ranges up to 8MB: single ReadAt creates ONE bounded reader
	// covering only the needed segments instead of offset→EOF.
	const maxSingleRead = 8 * 1024 * 1024
	if rangeLen <= maxSingleRead {
		bufp := fetchBufPool.Get().(*[]byte)
		buf := *bufp
		if int64(cap(buf)) < rangeLen {
			buf = make([]byte, rangeLen)
		} else {
			buf = buf[:rangeLen]
		}

		n, err := file.ReadAt(buf, start)
		if n > 0 {
			if _, writeErr := cf.item.WriteAt(buf[:n], start); writeErr != nil {
				*bufp = buf
				fetchBufPool.Put(bufp)
				return fmt.Errorf("write to cache at %d: %w", start, writeErr)
			}
		}
		*bufp = buf
		fetchBufPool.Put(bufp)
		if err != nil && err != io.EOF {
			return fmt.Errorf("readat backend [%d, %d): %w", start, end, err)
		}
		return nil
	}

	// For very large ranges (>8MB, defensive): chunked ReadAt
	const chunkLen = 4 * 1024 * 1024
	bufp := fetchBufPool.Get().(*[]byte)
	buf := *bufp
	if int64(cap(buf)) < chunkLen {
		buf = make([]byte, chunkLen)
	}
	for pos := start; pos < end; {
		toRead := min(int64(chunkLen), end-pos)
		n, readErr := file.ReadAt(buf[:toRead], pos)
		if n > 0 {
			if _, writeErr := cf.item.WriteAt(buf[:n], pos); writeErr != nil {
				*bufp = buf
				fetchBufPool.Put(bufp)
				return fmt.Errorf("write to cache at %d: %w", pos, writeErr)
			}
			pos += int64(n)
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			*bufp = buf
			fetchBufPool.Put(bufp)
			return fmt.Errorf("readat backend at %d: %w", pos, readErr)
		}
	}
	*bufp = buf
	fetchBufPool.Put(bufp)
	return nil
}

// Close releases the cached file.
func (cf *CachedFile) Close() error {
	if !cf.closed.CompareAndSwap(false, true) {
		return nil // Already closed
	}

	cf.item.Close()
	return nil
}

// Size returns the full file size.
func (cf *CachedFile) Size() int64 {
	return cf.size
}

// CachedBytes returns the number of bytes currently in cache.
func (cf *CachedFile) CachedBytes() int64 {
	return cf.item.CachedSize()
}
