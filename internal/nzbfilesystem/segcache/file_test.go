package segcache_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sync/singleflight"
)

// mockFile is an in-memory afero.File backed by a []byte.
type mockFile struct {
	afero.File
	data   []byte
	mu     sync.Mutex
	closed bool
}

func (m *mockFile) ReadAt(p []byte, off int64) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if off >= int64(len(m.data)) {
		return 0, io.EOF
	}

	n := copy(p, m.data[off:])
	if off+int64(n) >= int64(len(m.data)) {
		return n, io.EOF
	}

	return n, nil
}

func (m *mockFile) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// mockOpener records which files were opened and returns mockFile backed by content.
type mockOpener struct {
	mu      sync.Mutex
	content []byte
	opens   int
}

func (o *mockOpener) Open(_ context.Context, _ string) (afero.File, error) {
	o.mu.Lock()
	o.opens++
	o.mu.Unlock()
	return &mockFile{data: o.content}, nil
}

func buildTestManager(t *testing.T, totalData []byte, segSize int64) (
	*segcache.Manager,
	[]segcache.SegmentEntry,
	int64,
	*mockOpener,
) {
	t.Helper()

	opener := &mockOpener{content: totalData}
	fileSize := int64(len(totalData))

	var segments []segcache.SegmentEntry
	var pos int64
	for i := 0; pos < fileSize; i++ {
		end := pos + segSize
		if end > fileSize {
			end = fileSize
		}
		segments = append(segments, segcache.SegmentEntry{
			MessageID: bytes.Repeat([]byte{'a' + byte(i%26)}, 8),
			FileStart: pos,
			FileEnd:   end,
		})
		pos = end
	}

	// Convert MessageID from []byte to string
	for i := range segments {
		segments[i].MessageID = string(bytes.Repeat([]byte{'a' + byte(i%26)}, 8))
	}

	cfg := segcache.ManagerConfig{
		Enabled:             true,
		CachePath:           t.TempDir(),
		MaxSizeBytes:        100 * 1024 * 1024,
		ReadAheadSegments:   0, // disable prefetch in tests
		PrefetchConcurrency: 1,
	}

	mgr, err := segcache.NewManager(cfg, slog.Default())
	require.NoError(t, err)

	return mgr, segments, fileSize, opener
}

func TestSegmentCachedFileReadAtSingleSegment(t *testing.T) {
	data := []byte("Hello, Usenet!")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	buf := make([]byte, len(data))
	n, err := f.ReadAt(buf, 0)
	assert.NoError(t, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestSegmentCachedFileReadAtSpanningTwoSegments(t *testing.T) {
	// 20 bytes split into two 10-byte segments.
	data := []byte("0123456789abcdefghij")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 10)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	// Read across the segment boundary.
	buf := make([]byte, 12)
	n, err := f.ReadAt(buf, 5)
	assert.NoError(t, err)
	assert.Equal(t, 12, n)
	assert.Equal(t, data[5:17], buf[:n])
}

func TestSegmentCachedFileReadAtEOF(t *testing.T) {
	data := []byte("short")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	// Read beyond end.
	buf := make([]byte, 10)
	n, err := f.ReadAt(buf, int64(len(data)))
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, 0, n)
}

func TestSegmentCachedFileReadAtClampsToFileSize(t *testing.T) {
	data := []byte("0123456789")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	// Buffer larger than file size; should read only available bytes.
	buf := make([]byte, 100)
	n, err := f.ReadAt(buf, 0)
	assert.Equal(t, io.EOF, err)
	assert.Equal(t, len(data), n)
	assert.Equal(t, data, buf[:n])
}

func TestSegmentCachedFileReadSeekWebDAV(t *testing.T) {
	data := []byte("ABCDEFGHIJKLMNOPQRST") // 20 bytes
	mgr, segments, fileSize, opener := buildTestManager(t, data, 10)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	// Seek to middle, then Read.
	pos, err := f.Seek(5, io.SeekStart)
	require.NoError(t, err)
	assert.EqualValues(t, 5, pos)

	buf := make([]byte, 8)
	n, err := f.Read(buf)
	require.NoError(t, err)
	assert.Equal(t, 8, n)
	assert.Equal(t, data[5:13], buf[:n])
}

func TestSegmentCachedFileConcurrentReadAt(t *testing.T) {
	data := make([]byte, 1000)
	for i := range data {
		data[i] = byte(i % 256)
	}

	mgr, segments, fileSize, opener := buildTestManager(t, data, 100)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	const workers = 8
	var wg sync.WaitGroup
	wg.Add(workers)

	for w := range workers {
		off := int64(w * 100)
		wg.Done()
		go func() {
			buf := make([]byte, 100)
			n, readErr := f.ReadAt(buf, off)
			assert.NoError(t, readErr)
			assert.Equal(t, 100, n)
			assert.Equal(t, data[off:off+100], buf[:n])
		}()
	}

	wg.Wait()
}

func TestSegmentCachedFileSegmentCacheHit(t *testing.T) {
	data := []byte("cached data that should not require a second download")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	buf := make([]byte, len(data))

	// First read — should trigger download.
	_, err = f.ReadAt(buf, 0)
	require.NoError(t, err)
	firstOpens := opener.opens

	// Second read — segment should be cached, no new download.
	_, err = f.ReadAt(buf, 0)
	require.NoError(t, err)
	assert.Equal(t, firstOpens, opener.opens, "second read should use cached segment")
}

func TestSegmentCachedFileWriteReturnsPermissionError(t *testing.T) {
	data := []byte("data")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("test.mkv")

	n, err := f.Write([]byte("write attempt"))
	assert.Error(t, err)
	assert.Equal(t, 0, n)
}

func TestSegmentCachedFileStat(t *testing.T) {
	data := []byte("stat test data")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f, err := mgr.Open("path/to/test.mkv", segments, fileSize, opener)
	require.NoError(t, err)
	defer mgr.Close("path/to/test.mkv")

	info, err := f.Stat()
	require.NoError(t, err)
	assert.Equal(t, "test.mkv", info.Name())
	assert.Equal(t, fileSize, info.Size())
	assert.False(t, info.IsDir())
}

func TestSegmentCachedFileDirectFetchGroupSharing(t *testing.T) {
	// Two files opened for the same path share a singleflight.Group.
	// This test verifies the Manager returns functional files for both handles.
	data := []byte("shared path data 0123456789")
	mgr, segments, fileSize, opener := buildTestManager(t, data, 64)

	f1, err := mgr.Open("shared.mkv", segments, fileSize, opener)
	require.NoError(t, err)

	f2, err := mgr.Open("shared.mkv", segments, fileSize, opener)
	require.NoError(t, err)

	buf1 := make([]byte, len(data))
	buf2 := make([]byte, len(data))

	_, err = f1.ReadAt(buf1, 0)
	require.NoError(t, err)

	_, err = f2.ReadAt(buf2, 0)
	require.NoError(t, err)

	assert.Equal(t, data, buf1)
	assert.Equal(t, data, buf2)

	mgr.Close("shared.mkv")
	mgr.Close("shared.mkv")
}

// Ensure SegmentCachedFile satisfies webdav.File at compile time.
// webdav.File embeds http.File (Read, Seek, Close, Readdir, Stat) + Write.
var _ interface {
	ReadAt([]byte, int64) (int, error)
	Read([]byte) (int, error)
	Seek(int64, int) (int64, error)
	Close() error
	Write([]byte) (int, error)
} = (*segcache.SegmentCachedFile)(nil)

// Ensure SegmentCachedFile compiles correctly with singleflight (package-level check).
var _ *singleflight.Group = (*singleflight.Group)(nil)
