package fuse

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// Mock File
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

// Mock FS
type MockFs struct {
	mock.Mock
	afero.Fs
}

func (m *MockFs) Open(name string) (afero.File, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(afero.File), args.Error(1)
}

func (m *MockFs) Stat(name string) (os.FileInfo, error) {
	args := m.Called(name)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(os.FileInfo), args.Error(1)
}

// Mock FileInfo
type MockFileInfo struct {
	name    string
	size    int64
	mode    os.FileMode
	modTime time.Time
	isDir   bool
}

func (m *MockFileInfo) Name() string       { return m.name }
func (m *MockFileInfo) Size() int64        { return m.size }
func (m *MockFileInfo) Mode() os.FileMode  { return m.mode }
func (m *MockFileInfo) ModTime() time.Time { return m.modTime }
func (m *MockFileInfo) IsDir() bool        { return m.isDir }
func (m *MockFileInfo) Sys() interface{}   { return nil }

// TestHandle_Read_Concurrency tests that Handle correctly serializes
// concurrent read requests using Seek+Read with mutex protection in fallback mode.
func TestHandle_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Seek", int64(100), 0).Return(int64(100), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	mockFile.On("Seek", int64(200), 0).Return(int64(200), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	handle := &Handle{
		file:     mockFile,
		logger:   logger,
		path:     "testfile",
		position: 0,
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, status := handle.Read(ctx, dest, 100)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	go func() {
		defer wg.Done()
		_, status := handle.Read(ctx, dest, 200)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	wg.Wait()

	mockFile.AssertExpectations(t)
}

// TestHandle_Read_SeekError tests that Handle returns EIO when Seek fails
func TestHandle_Read_SeekError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Seek", int64(100), 0).Return(int64(0), os.ErrPermission).Once()

	handle := &Handle{
		file:     mockFile,
		logger:   logger,
		path:     "testfile",
		position: 0,
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 100)
	assert.Equal(t, syscall.EIO, status)

	mockFile.AssertExpectations(t)
}

// TestHandle_Read_ReadError tests that Handle returns EIO when Read fails
func TestHandle_Read_ReadError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(0, os.ErrPermission).Once()

	handle := &Handle{
		file:     mockFile,
		logger:   logger,
		path:     "testfile",
		position: 0,
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.EIO, status)

	mockFile.AssertExpectations(t)
}

// TestHandle_Read_SequentialSkipsSeek tests that sequential reads skip unnecessary seeks.
func TestHandle_Read_SequentialSkipsSeek(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Times(3)

	handle := &Handle{
		file:     mockFile,
		logger:   logger,
		path:     "testfile",
		position: 0,
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	// Read 1: offset 0, position 0 -> no seek
	result, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	// Read 2: offset 10, position 10 -> no seek
	result, status = handle.Read(ctx, dest, 10)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	// Read 3: offset 20, position 20 -> no seek
	result, status = handle.Read(ctx, dest, 20)
	assert.Equal(t, syscall.Errno(0), status)
	assert.NotNil(t, result)

	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Seek", mock.Anything, mock.Anything)
}

// TestHandle_Read_RandomSeeksWhenNeeded tests that random reads trigger seeks
func TestHandle_Read_RandomSeeksWhenNeeded(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at 0: no seek
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	// Random seek to 1000: needs seek
	mockFile.On("Seek", int64(1000), 0).Return(int64(1000), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	// Jump back to 500: needs seek
	mockFile.On("Seek", int64(500), 0).Return(int64(500), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	handle := &Handle{
		file:     mockFile,
		logger:   logger,
		path:     "testfile",
		position: 0,
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	// Sequential read at 0
	_, status := handle.Read(ctx, dest, 0)
	assert.Equal(t, syscall.Errno(0), status)

	// Random seek forward to 1000
	_, status = handle.Read(ctx, dest, 1000)
	assert.Equal(t, syscall.Errno(0), status)

	// Random seek backward to 500
	_, status = handle.Read(ctx, dest, 500)
	assert.Equal(t, syscall.Errno(0), status)

	mockFile.AssertExpectations(t)
}

func TestDir_Getattr(t *testing.T) {
	logger := slog.Default()

	// Test root directory
	root := NewDir(nil, "", logger, 1000, 1000, nil, nil)
	ctx := context.Background()
	out := &fuse.AttrOut{}
	errno := root.Getattr(ctx, nil, out)

	assert.Equal(t, syscall.Errno(0), errno)
	assert.True(t, out.Mode&syscall.S_IFDIR != 0)
	assert.Equal(t, uint32(0755|syscall.S_IFDIR), out.Mode)
	assert.Equal(t, uint32(1000), out.Uid)
	assert.Equal(t, uint32(1000), out.Gid)
}

func TestHandle_Release_Idempotent(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	mockFile.On("Close").Return(nil).Once()

	handle := &Handle{
		file:   mockFile,
		logger: logger,
		path:   "testfile",
	}

	ctx := context.Background()

	// First release should close
	errno := handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	// Second release should be a no-op (atomic CompareAndSwap)
	errno = handle.Release(ctx)
	assert.Equal(t, syscall.Errno(0), errno)

	// Close should only be called once
	mockFile.AssertNumberOfCalls(t, "Close", 1)
}
