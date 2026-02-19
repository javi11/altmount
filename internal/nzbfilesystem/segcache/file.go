package segcache

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"
	"golang.org/x/sync/singleflight"
)

// SegmentEntry describes a single Usenet segment in file-coordinate space.
// FileStart and FileEnd are cumulative byte offsets within the logical file.
type SegmentEntry struct {
	MessageID string
	FileStart int64 // inclusive
	FileEnd   int64 // exclusive
}

// FileOpener can open a named virtual file for reading.
// *nzbfilesystem.NzbFilesystem satisfies this interface.
type FileOpener interface {
	Open(ctx context.Context, name string) (afero.File, error)
}

// SegmentCachedFile provides ReadAt (for FUSE) and Read/Seek/Close (for WebDAV).
// It satisfies the webdav.File interface so it can be returned directly from
// the WebDAV filesystem's OpenFile method.
type SegmentCachedFile struct {
	path       string
	fileSize   int64
	segments   []SegmentEntry
	cache      *SegmentCache
	opener     FileOpener
	prefetcher *Prefetcher
	fetchGroup *singleflight.Group
	closed     atomic.Bool
	logger     *slog.Logger

	// Sequential read position for WebDAV Read()/Seek() access.
	mu       sync.Mutex
	position int64
}

// ReadAt reads len(p) bytes starting at file offset off.
// Thread-safe; multiple goroutines may call ReadAt concurrently.
func (f *SegmentCachedFile) ReadAt(p []byte, off int64) (int, error) {
	if f.closed.Load() {
		return 0, os.ErrClosed
	}

	if off >= f.fileSize {
		return 0, io.EOF
	}

	end := off + int64(len(p))
	if end > f.fileSize {
		end = f.fileSize
		p = p[:end-off]
	}

	firstIdx := f.findSegmentForOffset(off)
	if firstIdx < 0 {
		return 0, io.EOF
	}

	var pOff int
	cur := off

	for i := firstIdx; i < len(f.segments) && cur < end; i++ {
		seg := f.segments[i]
		if seg.FileStart >= end {
			break
		}

		// Fetch (or wait for an in-flight fetch) using singleflight.
		_, fetchErr, _ := f.fetchGroup.Do(seg.MessageID, func() (any, error) {
			if f.cache.Has(seg.MessageID) {
				return nil, nil
			}
			return nil, f.fetchAndCache(seg)
		})
		if fetchErr != nil {
			return pOff, fetchErr
		}

		data, ok := f.cache.Get(seg.MessageID)
		if !ok {
			return pOff, fmt.Errorf("segcache: segment %s vanished after fetch", seg.MessageID)
		}

		segReadStart := cur - seg.FileStart
		available := data[segReadStart:]
		toCopy := int64(len(available))
		if remaining := end - cur; toCopy > remaining {
			toCopy = remaining
		}

		n := copy(p[pOff:], available[:toCopy])
		pOff += n
		cur += int64(n)
	}

	if f.prefetcher != nil {
		f.prefetcher.RecordAccess(firstIdx)
	}

	if pOff == 0 {
		return 0, io.EOF
	}

	if cur >= f.fileSize {
		return pOff, io.EOF
	}

	return pOff, nil
}

// findSegmentForOffset returns the index of the segment containing file offset off,
// or -1 if off is beyond all segments.
func (f *SegmentCachedFile) findSegmentForOffset(off int64) int {
	if len(f.segments) == 0 {
		return -1
	}

	// Binary search: largest i where segments[i].FileStart <= off.
	lo, hi := 0, len(f.segments)
	for lo < hi {
		mid := (lo + hi) / 2
		if f.segments[mid].FileStart <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}

	idx := lo - 1
	if idx < 0 {
		return 0
	}

	if f.segments[idx].FileEnd <= off {
		return -1
	}

	return idx
}

// fetchAndCache downloads and stores one segment via the FileOpener.
// It aligns the ReadAt call to segment boundaries so the underlying
// MetadataVirtualFile downloads exactly one NNTP article.
func (f *SegmentCachedFile) fetchAndCache(seg SegmentEntry) error {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	file, err := f.opener.Open(ctx, f.path)
	if err != nil {
		return fmt.Errorf("segcache: open %s for segment %s: %w", f.path, seg.MessageID, err)
	}
	defer func() { _ = file.Close() }()

	size := seg.FileEnd - seg.FileStart
	buf := make([]byte, size)

	n, err := file.ReadAt(buf, seg.FileStart)
	if err != nil && err != io.EOF {
		return fmt.Errorf("segcache: ReadAt(%d) segment %s: %w", seg.FileStart, seg.MessageID, err)
	}

	return f.cache.Put(seg.MessageID, buf[:n])
}

// Read implements io.Reader for sequential WebDAV access.
func (f *SegmentCachedFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	pos := f.position
	f.mu.Unlock()

	n, err := f.ReadAt(p, pos)
	if n > 0 {
		f.mu.Lock()
		f.position += int64(n)
		f.mu.Unlock()
	}

	return n, err
}

// Seek implements io.Seeker for WebDAV range requests.
func (f *SegmentCachedFile) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	var newPos int64
	switch whence {
	case io.SeekStart:
		newPos = offset
	case io.SeekCurrent:
		newPos = f.position + offset
	case io.SeekEnd:
		newPos = f.fileSize + offset
	default:
		return 0, fmt.Errorf("segcache: invalid Seek whence %d", whence)
	}

	if newPos < 0 {
		return 0, fmt.Errorf("segcache: negative seek position %d", newPos)
	}

	f.position = newPos
	return newPos, nil
}

// Close marks this file handle as closed.
func (f *SegmentCachedFile) Close() error {
	f.closed.Store(true)
	return nil
}

// Stat returns a synthetic os.FileInfo for this file.
func (f *SegmentCachedFile) Stat() (os.FileInfo, error) {
	return &segFileInfo{name: filepath.Base(f.path), size: f.fileSize}, nil
}

// Readdir returns an error; SegmentCachedFile is always a regular file.
func (f *SegmentCachedFile) Readdir(_ int) ([]os.FileInfo, error) {
	return nil, fmt.Errorf("segcache: Readdir called on regular file %s", f.path)
}

// Write returns ErrPermission; the underlying NZB filesystem is read-only.
func (f *SegmentCachedFile) Write(_ []byte) (int, error) {
	return 0, os.ErrPermission
}

// segFileInfo is a minimal os.FileInfo for a cached file.
type segFileInfo struct {
	name string
	size int64
}

func (fi *segFileInfo) Name() string       { return fi.name }
func (fi *segFileInfo) Size() int64        { return fi.size }
func (fi *segFileInfo) Mode() os.FileMode  { return 0o444 }
func (fi *segFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *segFileInfo) IsDir() bool        { return false }
func (fi *segFileInfo) Sys() any           { return nil }
