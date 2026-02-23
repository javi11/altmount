package hanwen

import (
	"context"
	"io"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"testing"

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

func TestHandle_Read_SeekAndRead(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at offset 100: must Seek then Read
	mockFile.On("Seek", int64(100), io.SeekStart).Return(int64(100), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	// Second read at offset 200: must Seek then Read (non-sequential)
	mockFile.On("Seek", int64(200), io.SeekStart).Return(int64(200), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 100)
	assert.Equal(t, syscall.Errno(0), status)

	_, status = handle.Read(ctx, dest, 200)
	assert.Equal(t, syscall.Errno(0), status)

	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
}

func TestHandle_Read_SequentialSkipsSeek(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at offset 0: position starts at 0, so no Seek needed
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Twice()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()
	dest := make([]byte, 10)

	// Read at offset 0 — position is 0, skip seek
	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)

	// Read at offset 10 — position is now 10 (0 + 10), skip seek
	_, status = handle.Read(ctx, dest, 10)
	assert.Equal(t, syscall.Errno(0), status)

	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Seek", mock.Anything, mock.Anything)
}

func TestHandle_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Concurrent reads are serialized by mutex — both will Seek+Read
	mockFile.On("Seek", mock.AnythingOfType("int64"), io.SeekStart).Return(int64(0), nil)
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

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
	mockFile.AssertExpectations(t)
}

func TestHandle_Read_ReadError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(0, os.ErrPermission).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.EIO, status)

	mockFile.AssertExpectations(t)
}

func TestHandle_Read_EOF(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(5, io.EOF).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()
	dest := make([]byte, 10)

	result, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	mockFile.AssertExpectations(t)
}

func TestHandle_Read_SeekError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Read at non-zero offset requires seek — which fails
	mockFile.On("Seek", int64(500), io.SeekStart).Return(int64(0), os.ErrInvalid).Once()

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 500)
	assert.Equal(t, syscall.EIO, status)

	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Read", mock.Anything)
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
