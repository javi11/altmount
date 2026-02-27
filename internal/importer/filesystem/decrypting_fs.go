package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"time"

	aescipher "github.com/javi11/altmount/internal/encryption/aes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
)

// Compile-time interface checks
var (
	_ fs.File   = (*DecryptingFile)(nil)
	_ io.Seeker = (*DecryptingFile)(nil)
	_ fs.FS     = (*DecryptingFileSystem)(nil)
)

// DecryptingFileEntry represents one file in the DecryptingFileSystem.
// It holds the outer RAR segments and optional AES credentials needed
// to read (and decrypt) the inner RAR volume at import time.
type DecryptingFileEntry struct {
	Filename      string
	Segments      []*metapb.SegmentData
	DecryptedSize int64  // Size of the decrypted data
	AesKey        []byte // Empty = no encryption
	AesIV         []byte
}

// DecryptingFileSystem implements fs.FS for reading inner RAR archives
// that are stored inside outer RAR archives on Usenet.
// When AES credentials are present, it decrypts on-the-fly using AES-CBC.
// When no credentials are present, it reads raw segments (like UsenetFileSystem).
type DecryptingFileSystem struct {
	ctx         context.Context
	poolManager pool.Manager
	files       map[string]DecryptingFileEntry
	maxPrefetch int
	readTimeout time.Duration
}

// NewDecryptingFileSystem creates a new filesystem for reading inner RAR volumes.
func NewDecryptingFileSystem(
	ctx context.Context,
	poolManager pool.Manager,
	entries []DecryptingFileEntry,
	maxPrefetch int,
	readTimeout time.Duration,
) *DecryptingFileSystem {
	filesMap := make(map[string]DecryptingFileEntry, len(entries))
	for _, e := range entries {
		filesMap[e.Filename] = e
	}

	return &DecryptingFileSystem{
		ctx:         ctx,
		poolManager: poolManager,
		files:       filesMap,
		maxPrefetch: maxPrefetch,
		readTimeout: readTimeout,
	}
}

// Open opens a file in the decrypting filesystem.
func (dfs *DecryptingFileSystem) Open(name string) (fs.File, error) {
	name = path.Clean(name)

	entry, ok := dfs.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}

	return &DecryptingFile{
		name:        name,
		entry:       &entry,
		poolManager: dfs.poolManager,
		ctx:         dfs.ctx,
		maxPrefetch: dfs.maxPrefetch,
		readTimeout: dfs.readTimeout,
	}, nil
}

// Stat returns file information. Implements rarlist.FileSystem.
func (dfs *DecryptingFileSystem) Stat(p string) (os.FileInfo, error) {
	p = filepath.Clean(p)

	entry, ok := dfs.files[p]
	if !ok {
		return nil, &fs.PathError{Op: "stat", Path: p, Err: fs.ErrNotExist}
	}

	return &UsenetFileInfo{
		name: filepath.Base(entry.Filename),
		size: entry.DecryptedSize,
	}, nil
}

// DecryptingFile implements fs.File and io.Seeker for reading an inner RAR volume.
type DecryptingFile struct {
	name        string
	entry       *DecryptingFileEntry
	poolManager pool.Manager
	ctx         context.Context
	maxPrefetch int
	readTimeout time.Duration

	reader   io.ReadCloser
	position int64
	closed   bool
}

func (df *DecryptingFile) Stat() (fs.FileInfo, error) {
	return &UsenetFileInfo{
		name: df.name,
		size: df.entry.DecryptedSize,
	}, nil
}

func (df *DecryptingFile) Read(p []byte) (int, error) {
	runtime.Gosched()

	select {
	case <-df.ctx.Done():
		return 0, df.ctx.Err()
	default:
	}

	if df.closed {
		return 0, fs.ErrClosed
	}

	if df.reader == nil {
		timeout := df.readTimeout
		if timeout <= 0 {
			timeout = 5 * time.Minute
		}
		ctx, cancel := context.WithTimeout(df.ctx, timeout)
		defer cancel()

		reader, err := df.createReader(ctx, df.position)
		if err != nil {
			return 0, fmt.Errorf("failed to create reader: %w", err)
		}
		df.reader = reader
	}

	n, err := df.reader.Read(p)
	df.position += int64(n)
	return n, err
}

func (df *DecryptingFile) Close() error {
	if df.closed {
		return nil
	}
	df.closed = true
	if df.reader != nil {
		return df.reader.Close()
	}
	return nil
}

func (df *DecryptingFile) Seek(offset int64, whence int) (int64, error) {
	if df.closed {
		return 0, fs.ErrClosed
	}

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = df.position + offset
	case io.SeekEnd:
		abs = df.entry.DecryptedSize + offset
	default:
		return 0, fmt.Errorf("invalid whence value: %d", whence)
	}

	if abs < 0 {
		return 0, fmt.Errorf("negative seek position: %d", abs)
	}

	if abs != df.position && df.reader != nil {
		df.reader.Close()
		df.reader = nil
	}

	df.position = abs
	return abs, nil
}

// createReader creates a reader starting at the given position.
// If AES credentials are present, it wraps the segment reader with AES-CBC decryption.
func (df *DecryptingFile) createReader(ctx context.Context, start int64) (io.ReadCloser, error) {
	entry := df.entry

	if len(entry.AesKey) > 0 {
		return df.createDecryptingReader(ctx, start)
	}

	return df.createPlainReader(ctx, start, entry.DecryptedSize-1)
}

// createPlainReader creates a Usenet reader without decryption.
func (df *DecryptingFile) createPlainReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	loader := dbSegmentLoader{segs: df.entry.Segments}
	if loader.GetSegmentCount() == 0 {
		return nil, fmt.Errorf("no segments to download")
	}

	rg := usenet.GetSegmentsInRange(ctx, start, end, loader)
	return usenet.NewUsenetReader(ctx, df.poolManager.GetPool, rg, df.maxPrefetch, df.poolManager, df.name, nil)
}

// createDecryptingReader creates a reader with AES-CBC decryption.
func (df *DecryptingFile) createDecryptingReader(ctx context.Context, start int64) (io.ReadCloser, error) {
	entry := df.entry
	cipher := aescipher.NewAesCipher()
	encryptedSize := cipher.EncryptedSize(entry.DecryptedSize)

	getReader := func(ctx context.Context, rStart, rEnd int64) (io.ReadCloser, error) {
		loader := dbSegmentLoader{segs: entry.Segments}
		if loader.GetSegmentCount() == 0 {
			return nil, fmt.Errorf("no segments to download")
		}

		// Clamp end to encrypted size
		if rEnd >= encryptedSize {
			rEnd = encryptedSize - 1
		}

		rg := usenet.GetSegmentsInRange(ctx, rStart, rEnd, loader)
		return usenet.NewUsenetReader(ctx, df.poolManager.GetPool, rg, df.maxPrefetch, df.poolManager, df.name, nil)
	}

	var rh *utils.RangeHeader
	if start > 0 {
		rh = &utils.RangeHeader{Start: start, End: entry.DecryptedSize - 1}
	}

	return cipher.Open(ctx, rh, entry.DecryptedSize, entry.AesKey, entry.AesIV, getReader)
}
