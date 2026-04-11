//go:build linux

package hanwen

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// MockFile implements afero.File + readAtContexter
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

func (m *MockFile) Write(p []byte) (n int, err error)                  { return 0, nil }
func (m *MockFile) WriteAt(p []byte, off int64) (n int, err error)     { return 0, nil }
func (m *MockFile) Name() string                                       { return "mock" }
func (m *MockFile) Readdir(count int) ([]os.FileInfo, error)           { return nil, nil }
func (m *MockFile) Readdirnames(n int) ([]string, error)               { return nil, nil }
func (m *MockFile) Stat() (os.FileInfo, error)                         { return nil, nil }
func (m *MockFile) Sync() error                                        { return nil }
func (m *MockFile) Truncate(size int64) error                          { return nil }
func (m *MockFile) WriteString(s string) (ret int, err error)          { return 0, nil }

func TestHandle_Read_UsesReadAtContext(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(0)).Return(16, nil).Once()
	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(65536)).Return(16, nil).Once()
	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(4096)).Return(16, nil).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
	ctx := context.Background()
	dest := make([]byte, 16)

	// Sequential read
	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)

	// Jump forward
	_, status = handle.Read(ctx, dest, 65536)
	assert.Equal(t, syscall.Errno(0), status)

	// Jump backward
	_, status = handle.Read(ctx, dest, 4096)
	assert.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	// No Seek or Read calls — only ReadAtContext
	mockFile.AssertNotCalled(t, "Seek", mock.Anything, mock.Anything)
	mockFile.AssertNotCalled(t, "Read", mock.Anything)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
}

func TestHandle_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), mock.AnythingOfType("int64")).Return(10, nil)
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
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

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(0)).Return(0, os.ErrPermission).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
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

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(0)).Return(5, io.EOF).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	result, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
}

func TestHandle_Read_ContextCanceled(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("ReadAtContext", mock.Anything, mock.AnythingOfType("[]uint8"), int64(0)).
		Return(0, context.Canceled).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.EINTR, status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
}

// readAtDepthFile counts overlapping ReadAtContext calls to verify serialization.
type readAtDepthFile struct {
	mu        sync.Mutex
	curDepth  int
	maxInRead int
	delay     time.Duration
}

func (f *readAtDepthFile) ReadAtContext(_ context.Context, p []byte, _ int64) (int, error) {
	f.mu.Lock()
	f.curDepth++
	if f.curDepth > f.maxInRead {
		f.maxInRead = f.curDepth
	}
	f.mu.Unlock()

	time.Sleep(f.delay)

	f.mu.Lock()
	f.curDepth--
	f.mu.Unlock()
	return len(p), nil
}

func (f *readAtDepthFile) ReadAt(p []byte, off int64) (int, error) { return 0, nil }
func (f *readAtDepthFile) Close() error                            { return nil }
func (f *readAtDepthFile) Read(p []byte) (int, error)              { return 0, nil }
func (f *readAtDepthFile) Seek(int64, int) (int64, error)          { return 0, nil }
func (f *readAtDepthFile) Write(p []byte) (int, error)             { return 0, nil }
func (f *readAtDepthFile) WriteAt(p []byte, off int64) (int, error) {
	return 0, nil
}
func (f *readAtDepthFile) Name() string                            { return "depth" }
func (f *readAtDepthFile) Readdir(int) ([]os.FileInfo, error)      { return nil, nil }
func (f *readAtDepthFile) Readdirnames(int) ([]string, error)      { return nil, nil }
func (f *readAtDepthFile) Stat() (os.FileInfo, error)              { return nil, nil }
func (f *readAtDepthFile) Sync() error                             { return nil }
func (f *readAtDepthFile) Truncate(int64) error                    { return nil }
func (f *readAtDepthFile) WriteString(string) (int, error)         { return 0, nil }

func TestHandle_Read_ConcurrentReadAtSerialized(t *testing.T) {
	df := &readAtDepthFile{delay: 5 * time.Millisecond}
	handle := NewHandle(df, slog.Default(), "testfile", nil, nil)
	defer handle.Release(context.Background())

	var wg sync.WaitGroup
	const n = 8
	var successes atomic.Int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(off int64) {
			defer wg.Done()
			buf := make([]byte, 16)
			_, errno := handle.Read(context.Background(), buf, off)
			if errno == 0 {
				successes.Add(1)
			}
		}(int64(i) * 4096)
	}
	wg.Wait()

	require.Equal(t, int32(n), successes.Load())
	assert.Equal(t, 1, df.maxInRead, "per-handle mutex should prevent overlapping ReadAtContext")
}

func TestHandle_Release_Idempotent(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Close").Return(nil).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()

	errno := handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	// Second release should be a no-op
	errno = handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	mockFile.AssertNumberOfCalls(t, "Close", 1)
}
