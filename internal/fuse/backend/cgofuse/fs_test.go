//go:build darwin

package cgofuse

import (
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/fuse/backend"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestFS builds a minimal FS for unit testing without mounting.
func newTestFS() *FS {
	return &FS{
		cfg:     backend.Config{},
		logger:  slog.Default(),
		handles: make(map[uint64]*openHandle),
		nextFH:  1,
		ready:   make(chan struct{}),
	}
}

func injectHandle(fs *FS, fh uint64, file afero.File) {
	fs.mu.Lock()
	fs.handles[fh] = &openHandle{file: file, path: "testfile"}
	fs.mu.Unlock()
}

// TestFS_Read_SmallForwardGapDoesNotSeek is the RED test for the forward-skip
// optimization in the cgofuse (macOS) backend.
//
// When the requested offset is slightly ahead of the current position (gap ≤
// maxForwardSkip), Read should bridge the gap by reading and discarding bytes
// instead of calling Seek.
//
// Calling Seek on MetadataVirtualFile destroys the UsenetReader prefetch
// pipeline, causing intermittent video streaming glitches.
//
// Currently FAILING (RED): Read calls Seek for any offset mismatch.
// Will pass (GREEN) after the forward-skip optimization is implemented.
func TestFS_Read_SmallForwardGapDoesNotSeek(t *testing.T) {
	const firstReadSize = 512
	const gap = 1024 // well within maxForwardSkip (4 MB)
	const secondReadSize = 512

	totalSize := int64(firstReadSize + gap + secondReadSize)
	data := make([]byte, totalSize)
	for i := range data {
		data[i] = byte(i % 251)
	}

	f := &cgoSeekCountingFile{data: data}
	fs := newTestFS()
	const fh = uint64(1)
	injectHandle(fs, fh, f)

	// First read at offset 0 — sequential, no seek needed.
	buf1 := make([]byte, firstReadSize)
	n := fs.Read("testfile", buf1, 0, fh)
	require.Equal(t, firstReadSize, n, "first read must succeed")

	// Second read at offset firstReadSize+gap — small forward gap.
	// After fix: gap drained via Read, no Seek called.
	buf2 := make([]byte, secondReadSize)
	n = fs.Read("testfile", buf2, int64(firstReadSize+gap), fh)
	require.Equal(t, secondReadSize, n, "second read must succeed")

	f.mu.Lock()
	seekCount := f.seekCount
	f.mu.Unlock()

	assert.Equal(t, 0, seekCount,
		"Seek must not be called for a small forward gap (%d bytes); "+
			"got %d call(s). Seek destroys the UsenetReader prefetch pipeline.",
		gap, seekCount)
}

// TestFS_Read_LargeForwardGapSeeks verifies that gaps larger than maxForwardSkip
// still use Seek.
func TestFS_Read_LargeForwardGapSeeks(t *testing.T) {
	const firstReadSize = 512
	const gap = maxForwardSkip + 1

	totalSize := int64(firstReadSize + gap + 512)
	data := make([]byte, totalSize)

	f := &cgoSeekCountingFile{data: data}
	fs := newTestFS()
	const fh = uint64(1)
	injectHandle(fs, fh, f)

	buf1 := make([]byte, firstReadSize)
	n := fs.Read("testfile", buf1, 0, fh)
	require.Equal(t, firstReadSize, n)

	buf2 := make([]byte, 512)
	n = fs.Read("testfile", buf2, int64(firstReadSize+gap), fh)
	require.Equal(t, 512, n)

	f.mu.Lock()
	seekCount := f.seekCount
	f.mu.Unlock()

	assert.Equal(t, 1, seekCount,
		"Seek must be called once for a large forward gap (%d bytes)", gap)
}

// TestFS_Read_BackwardSeekAlwaysSeeks verifies backward seeks always use Seek.
func TestFS_Read_BackwardSeekAlwaysSeeks(t *testing.T) {
	data := make([]byte, 4096)
	f := &cgoSeekCountingFile{data: data}
	fs := newTestFS()
	const fh = uint64(1)
	injectHandle(fs, fh, f)

	dest := make([]byte, 256)

	// Read at offset 2000 first (needs initial seek from 0)
	n := fs.Read("testfile", dest, 2000, fh)
	require.Equal(t, 256, n)

	// Read backward to offset 100 — must Seek
	n = fs.Read("testfile", dest, 100, fh)
	require.Equal(t, 256, n)

	f.mu.Lock()
	seekCount := f.seekCount
	f.mu.Unlock()

	assert.GreaterOrEqual(t, seekCount, 1, "backward seek must call Seek at least once")
}

// cgoSeekCountingFile is an in-memory afero.File that counts Seek calls.
type cgoSeekCountingFile struct {
	mu        sync.Mutex
	seekCount int
	data      []byte
	pos       int64
}

func (f *cgoSeekCountingFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pos >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *cgoSeekCountingFile) Seek(offset int64, whence int) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seekCount++
	switch whence {
	case io.SeekStart:
		f.pos = offset
	case io.SeekCurrent:
		f.pos += offset
	case io.SeekEnd:
		f.pos = int64(len(f.data)) + offset
	}
	return f.pos, nil
}

func (f *cgoSeekCountingFile) Close() error                             { return nil }
func (f *cgoSeekCountingFile) ReadAt(p []byte, off int64) (int, error)  { return 0, nil }
func (f *cgoSeekCountingFile) Write(p []byte) (int, error)              { return 0, nil }
func (f *cgoSeekCountingFile) WriteAt(p []byte, off int64) (int, error) { return 0, nil }
func (f *cgoSeekCountingFile) Name() string                             { return "seekcounting" }
func (f *cgoSeekCountingFile) Readdir(count int) ([]os.FileInfo, error) { return nil, nil }
func (f *cgoSeekCountingFile) Readdirnames(n int) ([]string, error)     { return nil, nil }
func (f *cgoSeekCountingFile) Stat() (os.FileInfo, error)               { return nil, nil }
func (f *cgoSeekCountingFile) Sync() error                              { return nil }
func (f *cgoSeekCountingFile) Truncate(size int64) error                { return nil }
func (f *cgoSeekCountingFile) WriteString(s string) (int, error)        { return 0, nil }
