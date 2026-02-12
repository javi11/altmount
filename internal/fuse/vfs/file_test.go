package vfs

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockFile implements afero.File for testing.
type mockFile struct {
	data     []byte
	pos      int64
	isClosed bool
}

func (f *mockFile) Close() error               { f.isClosed = true; return nil }
func (f *mockFile) Name() string                { return "mock" }
func (f *mockFile) Stat() (os.FileInfo, error)  { return nil, nil }
func (f *mockFile) Sync() error                 { return nil }
func (f *mockFile) Truncate(int64) error        { return nil }
func (f *mockFile) WriteString(string) (int, error) { return 0, nil }
func (f *mockFile) Write([]byte) (int, error)   { return 0, nil }
func (f *mockFile) WriteAt([]byte, int64) (int, error) { return 0, nil }
func (f *mockFile) Readdir(int) ([]os.FileInfo, error) { return nil, nil }
func (f *mockFile) Readdirnames(int) ([]string, error) { return nil, nil }
func (f *mockFile) ReadAt(p []byte, off int64) (int, error) {
	if off >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[off:])
	return n, nil
}

func (f *mockFile) Read(p []byte) (int, error) {
	if f.pos >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *mockFile) Seek(offset int64, whence int) (int64, error) {
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

// mockOpener implements FileOpener for testing.
type mockOpener struct {
	data []byte
}

func (o *mockOpener) Open(_ context.Context, _ string) (afero.File, error) {
	return &mockFile{data: o.data}, nil
}

func TestCachedFile_ReadAt_CacheHit(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ChunkSize = 16
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := bytes.Repeat([]byte("x"), 100)
	opener := &mockOpener{data: data}

	item := c.GetOrCreate("/test.mkv", 100)
	cf, err := NewCachedFile(item, opener, "/test.mkv", 100, 16, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	// First read: cache miss, fetches from backend
	buf := make([]byte, 10)
	n, err := cf.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, data[:10], buf)

	// Second read from same range: cache hit
	buf2 := make([]byte, 10)
	n, err = cf.ReadAt(buf2, 0)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, data[:10], buf2)
}

func TestCachedFile_ReadAt_CacheMiss(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ChunkSize = 16
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	opener := &mockOpener{data: data}

	item := c.GetOrCreate("/test.mkv", 100)
	cf, err := NewCachedFile(item, opener, "/test.mkv", 100, 16, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	// Read at offset 50
	buf := make([]byte, 10)
	n, err := cf.ReadAt(buf, 50)
	require.NoError(t, err)
	assert.Equal(t, 10, n)
	assert.Equal(t, data[50:60], buf)
}

func TestCachedFile_ReadAt_EOF(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := []byte("hello")
	opener := &mockOpener{data: data}

	item := c.GetOrCreate("/test.txt", 5)
	cf, err := NewCachedFile(item, opener, "/test.txt", 5, 64, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	buf := make([]byte, 10)
	_, err = cf.ReadAt(buf, 100)
	assert.ErrorIs(t, err, io.EOF)
}

func TestCachedFile_ReadAt_ClampToSize(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := []byte("hello world!")
	opener := &mockOpener{data: data}

	item := c.GetOrCreate("/test.txt", int64(len(data)))
	cf, err := NewCachedFile(item, opener, "/test.txt", int64(len(data)), 64, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	// Read beyond end of file — should be clamped
	buf := make([]byte, 20)
	n, err := cf.ReadAt(buf, 5)
	require.NoError(t, err)
	assert.Equal(t, 7, n)
	assert.Equal(t, data[5:], buf[:7])
}

func TestCachedFile_Concurrent_Reads(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ChunkSize = 16
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := make([]byte, 200)
	for i := range data {
		data[i] = byte(i)
	}
	opener := &mockOpener{data: data}

	item := c.GetOrCreate("/test.mkv", 200)
	cf, err := NewCachedFile(item, opener, "/test.mkv", 200, 16, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			off := int64(i * 15)
			buf := make([]byte, 10)
			n, err := cf.ReadAt(buf, off)
			assert.NoError(t, err)
			assert.Equal(t, 10, n)
			assert.Equal(t, data[off:off+10], buf)
		}()
	}
	wg.Wait()
}

type trackingMockFile struct {
	mockFile
	readAtCalls int
	seekCalls   int
	readCalls   int
}

func (f *trackingMockFile) ReadAt(p []byte, off int64) (int, error) {
	f.readAtCalls++
	return f.mockFile.ReadAt(p, off)
}

func (f *trackingMockFile) Seek(offset int64, whence int) (int64, error) {
	f.seekCalls++
	return f.mockFile.Seek(offset, whence)
}

func (f *trackingMockFile) Read(p []byte) (int, error) {
	f.readCalls++
	return f.mockFile.Read(p)
}

type trackingOpener struct {
	data  []byte
	files []*trackingMockFile
}

func (o *trackingOpener) Open(_ context.Context, _ string) (afero.File, error) {
	f := &trackingMockFile{mockFile: mockFile{data: o.data}}
	o.files = append(o.files, f)
	return f, nil
}

func TestCachedFile_UsesReadAt_NotSeekRead(t *testing.T) {
	cfg := testCacheConfig(t)
	cfg.ChunkSize = 16
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	data := make([]byte, 100)
	for i := range data {
		data[i] = byte(i)
	}
	opener := &trackingOpener{data: data}

	item := c.GetOrCreate("/test.mkv", 100)
	cf, err := NewCachedFile(item, opener, "/test.mkv", 100, 16, slog.Default(), nil)
	require.NoError(t, err)
	defer cf.Close()

	buf := make([]byte, 10)
	n, err := cf.ReadAt(buf, 50)
	require.NoError(t, err)
	assert.Equal(t, 10, n)

	require.GreaterOrEqual(t, len(opener.files), 1)
	for _, f := range opener.files {
		assert.Greater(t, f.readAtCalls, 0, "ReadAt should have been called")
		assert.Equal(t, 0, f.seekCalls, "Seek should NOT have been called")
		assert.Equal(t, 0, f.readCalls, "Read should NOT have been called")
	}
}

func TestCachedFile_Close_Idempotent(t *testing.T) {
	cfg := testCacheConfig(t)
	c, err := NewCache(cfg, slog.Default())
	require.NoError(t, err)

	opener := &mockOpener{data: []byte("test")}
	item := c.GetOrCreate("/test.txt", 4)

	cf, err := NewCachedFile(item, opener, "/test.txt", 4, 64, slog.Default(), nil)
	require.NoError(t, err)

	assert.NoError(t, cf.Close())
	assert.NoError(t, cf.Close()) // Second close is idempotent
}
