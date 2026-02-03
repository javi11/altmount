package fuse

import (
	"context"
	"io"
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

func TestAltMountFile_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Setup expectations
	// We expect Seek then Read for each call

	// First read: offset 0, read 10 bytes
	mockFile.On("Seek", int64(0), io.SeekStart).Return(int64(0), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	// Second read: offset 100, read 10 bytes
	mockFile.On("Seek", int64(100), io.SeekStart).Return(int64(100), nil).Once()
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Once()

	handle := &FileHandle{
		file:   mockFile,
		logger: logger,
		path:   "testfile",
	}

	ctx := context.Background()
	dest := make([]byte, 10)

	// Execute reads concurrently
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, status := handle.Read(ctx, dest, 0)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	go func() {
		defer wg.Done()
		_, status := handle.Read(ctx, dest, 100)
		assert.Equal(t, syscall.Errno(0), status)
	}()

	wg.Wait()

	mockFile.AssertExpectations(t)
}

func TestAltMountRoot_Getattr(t *testing.T) {
	mockFs := new(MockFs)
	logger := slog.Default()

	root := NewAltMountRoot(mockFs, "/root", logger, 1000, 1000)
	root.isRootDir = false // Force it to check fs.Stat

	// Test Directory Getattr
	dirInfo := &MockFileInfo{name: "subdir", isDir: true, mode: 0755, size: 0}
	mockFs.On("Stat", "/root").Return(dirInfo, nil)

	ctx := context.Background()
	out := &fuse.AttrOut{}
	errno := root.Getattr(ctx, nil, out)

	assert.Equal(t, syscall.Errno(0), errno)
	assert.Equal(t, uint64(0), out.Size)
	assert.True(t, out.Mode&syscall.S_IFDIR != 0)
	assert.Equal(t, uint32(0755|syscall.S_IFDIR), out.Mode)
}
