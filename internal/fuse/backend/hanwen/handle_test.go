//go:build linux

package hanwen

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockFile implements afero.File
type MockFile struct {
	mock.Mock
}

func (m *MockFile) Close() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockFile) Read(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *MockFile) ReadAt(p []byte, off int64) (n int, err error) {
	args := m.Called(p, off)
	return args.Int(0), args.Error(1)
}

func (m *MockFile) ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error) {
	args := m.Called(ctx, p, off)
	return args.Int(0), args.Error(1)
}

func (m *MockFile) Seek(offset int64, whence int) (int64, error) {
	args := m.Called(offset, whence)
	return args.Get(0).(int64), args.Error(1)
}

func (m *MockFile) Write(p []byte) (n int, err error) {
	args := m.Called(p)
	return args.Int(0), args.Error(1)
}

func (m *MockFile) WriteAt(p []byte, off int64) (n int, err error) {
	args := m.Called(p, off)
	return args.Int(0), args.Error(1)
}

func (m *MockFile) Name() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockFile) Readdir(count int) ([]os.FileInfo, error) {
	args := m.Called(count)
	return args.Get(0).([]os.FileInfo), args.Error(1)
}

func (m *MockFile) Readdirnames(n int) ([]string, error) {
	args := m.Called(n)
	return args.Get(0).([]string), args.Error(1)
}

func (m *MockFile) Stat() (os.FileInfo, error) {
	args := m.Called()
	return args.Get(0).(os.FileInfo), args.Error(1)
}

func (m *MockFile) Sync() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockFile) Truncate(size int64) error {
	args := m.Called(size)
	return args.Error(0)
}

func (m *MockFile) WriteString(s string) (ret int, err error) {
	args := m.Called(s)
	return args.Int(0), args.Error(1)
}

// TestHandle_Read_InterleavedOffsets_BaselineSeekRead models kernel/player behavior: non-sequential
// offsets (e.g. read-ahead far ahead, then metadata/index probes). Baseline path is Seek+Read per jump;
// Phase 1+ will prefer io.ReaderAt when fuse.use_read_at is enabled.
func TestHandle_Read_InterleavedOffsets_BaselineSeekRead(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First at 0: position already 0 — no Seek.
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(16, nil).Once()
	// Jump to 64 KiB (read-ahead), then back to 4 KiB (typical non-seq pattern).
	mockFile.On("Seek", int64(65536), io.SeekStart).Return(int64(65536), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(16, nil).Once()
	mockFile.On("Seek", int64(4096), io.SeekStart).Return(int64(4096), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(16, nil).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 16)

	_, st := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), st)
	_, st = handle.Read(ctx, dest, 65536)
	assert.Equal(t, syscall.Errno(0), st)
	_, st = handle.Read(ctx, dest, 4096)
	assert.Equal(t, syscall.Errno(0), st)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
	mockFile.AssertNotCalled(t, "ReadAtContext", mock.Anything, mock.Anything, mock.Anything)
}

// TestHandle_Read_InterleavedOffsets_UsesReadAt verifies offset-native reads: no Seek,
// and ReadAtContext is used (MetadataVirtualFile implements it; mocks may implement both).
func TestHandle_Read_InterleavedOffsets_UsesReadAt(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(0)).Return(16, nil).Once()
	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(65536)).Return(16, nil).Once()
	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(4096)).Return(16, nil).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, true)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 16)

	_, st := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), st)
	_, st = handle.Read(ctx, dest, 65536)
	assert.Equal(t, syscall.Errno(0), st)
	_, st = handle.Read(ctx, dest, 4096)
	assert.Equal(t, syscall.Errno(0), st)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Seek", mock.Anything, mock.Anything)
	mockFile.AssertNotCalled(t, "Read", mock.Anything)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
}

func TestHandle_Read_SeekAndRead(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at offset 100: must Seek then Read
	mockFile.On("Seek", int64(100), io.SeekStart).Return(int64(100), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	// Second read at offset 200: must Seek then Read (non-sequential)
	mockFile.On("Seek", int64(200), io.SeekStart).Return(int64(200), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 100)
	assert.Equal(t, syscall.Errno(0), status)

	_, status = handle.Read(ctx, dest, 200)
	assert.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
	mockFile.AssertNotCalled(t, "ReadAtContext", mock.Anything, mock.Anything, mock.Anything)
}

func TestHandle_Read_SequentialSkipsSeek(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at offset 0: position starts at 0, so no Seek needed
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Twice()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	// Read at offset 0 — position is 0, skip seek
	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)

	// Read at offset 10 — position is now 10 (0 + 10), skip seek
	_, status = handle.Read(ctx, dest, 10)
	assert.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Seek", mock.Anything, mock.Anything)
}

// readAtDepthFile counts overlapping ReadAtContext calls (no pool / MVF).
type readAtDepthFile struct {
	mu        sync.Mutex
	inRead    int
	maxInRead int
}

func (f *readAtDepthFile) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	f.mu.Lock()
	f.inRead++
	if f.inRead > f.maxInRead {
		f.maxInRead = f.inRead
	}
	f.mu.Unlock()
	time.Sleep(8 * time.Millisecond)
	f.mu.Lock()
	f.inRead--
	f.mu.Unlock()
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}

func (f *readAtDepthFile) Close() error { return nil }
func (f *readAtDepthFile) Read(p []byte) (int, error)                       { return 0, io.EOF }
func (f *readAtDepthFile) ReadAt(p []byte, off int64) (int, error)          { return 0, nil }
func (f *readAtDepthFile) Seek(offset int64, whence int) (int64, error)     { return 0, nil }
func (f *readAtDepthFile) Write(p []byte) (int, error)                      { return 0, nil }
func (f *readAtDepthFile) WriteAt(p []byte, off int64) (int, error)         { return 0, nil }
func (f *readAtDepthFile) Name() string                                    { return "depth" }
func (f *readAtDepthFile) Readdir(count int) ([]os.FileInfo, error)         { return nil, nil }
func (f *readAtDepthFile) Readdirnames(n int) ([]string, error)             { return nil, nil }
func (f *readAtDepthFile) Stat() (os.FileInfo, error)                      { return nil, nil }
func (f *readAtDepthFile) Sync() error                                     { return nil }
func (f *readAtDepthFile) Truncate(size int64) error                        { return nil }
func (f *readAtDepthFile) WriteString(s string) (int, error)                { return 0, nil }

func TestHandle_Read_ConcurrentReadAtSerialized(t *testing.T) {
	df := &readAtDepthFile{}
	logger := slog.Default()
	handle := NewHandle(df, logger, "testfile", nil, nil, true)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 16)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, st := handle.Read(ctx, dest, 0)
		assert.Equal(t, syscall.Errno(0), st)
	}()
	go func() {
		defer wg.Done()
		_, st := handle.Read(ctx, dest, 100)
		assert.Equal(t, syscall.Errno(0), st)
	}()
	wg.Wait()

	assert.Equal(t, 1, df.maxInRead, "per-handle ReadAt mutex should prevent overlapping ReadAtContext")
}

func TestHandle_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Concurrent reads are serialized by the IO worker — both will Seek+Read
	mockFile.On("Seek", mock.AnythingOfType("int64"), io.SeekStart).Return(int64(0), nil)
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil)
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		dest := make([]byte, 10)
		_, status := handle.Read(ctx, dest, 100)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	go func() {
		defer wg.Done()
		dest := make([]byte, 10)
		_, status := handle.Read(ctx, dest, 200)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	wg.Wait()
	handle.Release(ctx)
	mockFile.AssertExpectations(t)
}

func TestHandle_Read_ReadError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(0, os.ErrPermission).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.EIO, status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
}

func TestHandle_Read_EOF(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(5, io.EOF).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	result, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
}

func TestHandle_Read_SeekError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Read at non-zero offset requires seek — which fails
	mockFile.On("Seek", int64(500), io.SeekStart).Return(int64(0), os.ErrInvalid).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 500)
	assert.Equal(t, syscall.EIO, status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Read", mock.Anything)
}

func TestHandle_Read_ContextCanceled(t *testing.T) {
	logger := slog.Default()

	bf := &blockingFile{readBlock: make(chan struct{})}
	handle := NewHandle(bf, logger, "testfile", nil, nil, false)
	// Release will close bf (unblocking any blocked worker) and stop the worker.
	defer handle.Release(context.Background())

	// Pre-cancelled context — should return EINTR promptly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	dest := make([]byte, 10)
	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.EINTR, status)
}

func TestHandle_Read_BlockingReadContextCanceled(t *testing.T) {
	logger := slog.Default()

	// blockingFile blocks on Read until Close is called (or readBlock is closed)
	bf := &blockingFile{readBlock: make(chan struct{})}
	handle := NewHandle(bf, logger, "testfile", nil, nil, false)
	// Release closes bf.Close() which unblocks the worker, then waits for it.
	defer handle.Release(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	dest := make([]byte, 10)

	done := make(chan syscall.Errno, 1)
	go func() {
		_, status := handle.Read(ctx, dest, 0)
		done <- status
	}()

	// Cancel context while Read is blocked in the worker
	cancel()

	status := <-done
	assert.Equal(t, syscall.EINTR, status)
}

func TestHandle_Read_BlockingSeekContextCanceled(t *testing.T) {
	logger := slog.Default()

	bf := &blockingFile{seekBlock: make(chan struct{})}
	handle := NewHandle(bf, logger, "testfile", nil, nil, false)
	defer handle.Release(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	dest := make([]byte, 10)

	done := make(chan syscall.Errno, 1)
	go func() {
		// offset 100 != position 0, so Seek will be called
		_, status := handle.Read(ctx, dest, 100)
		done <- status
	}()

	cancel()

	status := <-done
	assert.Equal(t, syscall.EINTR, status)
}

// blockingFile is a minimal afero.File that blocks on Read/Seek until Close is called.
// Used to test context cancellation while IO is in progress.
type blockingFile struct {
	readBlock chan struct{} // if non-nil, Read blocks until closed
	seekBlock chan struct{} // if non-nil, Seek blocks until closed
	closeOnce sync.Once
}

func (f *blockingFile) Read(p []byte) (int, error) {
	if f.readBlock != nil {
		<-f.readBlock
	}
	return 0, io.EOF
}

func (f *blockingFile) Seek(offset int64, whence int) (int64, error) {
	if f.seekBlock != nil {
		<-f.seekBlock
	}
	return offset, nil
}

// Close unblocks any goroutines waiting on readBlock or seekBlock, and is idempotent.
func (f *blockingFile) Close() error {
	f.closeOnce.Do(func() {
		if f.readBlock != nil {
			select {
			case <-f.readBlock: // already closed — receive succeeds immediately
			default:
				close(f.readBlock)
			}
		}
		if f.seekBlock != nil {
			select {
			case <-f.seekBlock:
			default:
				close(f.seekBlock)
			}
		}
	})
	return nil
}

func (f *blockingFile) ReadAt(p []byte, off int64) (int, error)      { return 0, nil }
func (f *blockingFile) Write(p []byte) (int, error)                  { return 0, nil }
func (f *blockingFile) WriteAt(p []byte, off int64) (int, error)     { return 0, nil }
func (f *blockingFile) Name() string                                 { return "blocking" }
func (f *blockingFile) Readdir(count int) ([]os.FileInfo, error)     { return nil, nil }
func (f *blockingFile) Readdirnames(n int) ([]string, error)         { return nil, nil }
func (f *blockingFile) Stat() (os.FileInfo, error)                   { return nil, nil }
func (f *blockingFile) Sync() error                                  { return nil }
func (f *blockingFile) Truncate(size int64) error                    { return nil }
func (f *blockingFile) WriteString(s string) (int, error)            { return 0, nil }

func TestHandle_Release_Idempotent(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Close").Return(nil).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil, false)

	ctx := context.Background()

	errno := handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	// Second release should be a no-op
	errno = handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	mockFile.AssertNumberOfCalls(t, "Close", 1)
}
