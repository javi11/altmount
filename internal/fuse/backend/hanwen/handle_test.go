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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
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

	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, off1)
	assert.Equal(t, syscall.Errno(0), status)

	_, status = handle.Read(ctx, dest, off2)
	assert.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "ReadAt", mock.Anything, mock.Anything)
}

func TestHandle_Read_SequentialSkipsSeek(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// First read at offset 0: position starts at 0, so no Seek needed
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil).Twice()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
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

func TestHandle_Read_Concurrency(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	// Concurrent reads are serialized by the IO worker — both will Seek+Read
	mockFile.On("Seek", mock.AnythingOfType("int64"), io.SeekStart).Return(int64(0), nil)
	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(10, nil)
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

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(0, os.ErrPermission).Once()
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

	mockFile.On("Read", mock.AnythingOfType("[]uint8")).Return(5, io.EOF).Once()
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

func TestHandle_Read_SeekError(t *testing.T) {
	mockFile := new(MockFile)
	logger := slog.Default()

	off := int64(500)
	mockFile.On("Seek", off, io.SeekStart).Return(int64(0), os.ErrInvalid).Once()
	mockFile.On("Close").Return(nil)

	handle := NewHandle(mockFile, logger, "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()
	dest := make([]byte, 10)

	_, status := handle.Read(ctx, dest, off)
	assert.Equal(t, syscall.EIO, status)

	handle.Release(ctx)
	mockFile.AssertExpectations(t)
	mockFile.AssertNotCalled(t, "Read", mock.Anything)
}

func TestHandle_Read_ContextCanceled(t *testing.T) {
	logger := slog.Default()

	bf := &blockingFile{readBlock: make(chan struct{})}
	handle := NewHandle(bf, logger, "testfile", nil, nil)
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
	handle := NewHandle(bf, logger, "testfile", nil, nil)
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
	handle := NewHandle(bf, logger, "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx, cancel := context.WithCancel(context.Background())
	dest := make([]byte, 10)

	done := make(chan syscall.Errno, 1)
	go func() {
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

// TestHandle_Read_NonSequentialAlwaysSeeks verifies that any non-sequential read
// calls Seek on the underlying file. The drain-forward optimization lives inside
// MetadataVirtualFile.Seek and is transparent to the FUSE handle layer.
func TestHandle_Read_NonSequentialAlwaysSeeks(t *testing.T) {
	const firstReadSize = 512
	const gap = 5 * 1024 * 1024 // 5 MiB non-sequential gap

	totalSize := int64(firstReadSize + gap + 512)
	data := make([]byte, totalSize)

	f := &seekCountingFile{data: data}
	handle := NewHandle(f, slog.Default(), "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()

	dest1 := make([]byte, firstReadSize)
	_, status := handle.Read(ctx, dest1, 0)
	require.Equal(t, syscall.Errno(0), status)

	dest2 := make([]byte, 512)
	_, status = handle.Read(ctx, dest2, int64(firstReadSize+gap))
	require.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)

	f.mu.Lock()
	seekCount := f.seekCount
	f.mu.Unlock()

	assert.Equal(t, 1, seekCount,
		"Seek must be called once for a non-sequential read (gap %d bytes)", gap)
}

// TestHandle_Read_BackwardSeekAlwaysSeeks verifies backward seeks always use Seek
// (there is no way to reverse a forward-only streaming reader).
func TestHandle_Read_BackwardSeekAlwaysSeeks(t *testing.T) {
	data := make([]byte, 4096)
	f := &seekCountingFile{data: data}
	handle := NewHandle(f, slog.Default(), "testfile", nil, nil)
	defer handle.Release(context.Background())

	ctx := context.Background()

	// Read at offset 2000 first (requires initial seek from 0)
	dest := make([]byte, 256)
	_, status := handle.Read(ctx, dest, 2000)
	require.Equal(t, syscall.Errno(0), status)

	// Now read backward to offset 100 — must Seek
	_, status = handle.Read(ctx, dest, 100)
	require.Equal(t, syscall.Errno(0), status)

	handle.Release(ctx)

	f.mu.Lock()
	seekCount := f.seekCount
	f.mu.Unlock()

	assert.GreaterOrEqual(t, seekCount, 1, "backward seek must call Seek at least once")
}

// seekCountingFile is a minimal in-memory afero.File that counts Seek calls.
// Used to verify the forward-skip optimization bypasses Seek for small gaps.
type seekCountingFile struct {
	mu        sync.Mutex
	seekCount int
	data      []byte
	pos       int64
}

func (f *seekCountingFile) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pos >= int64(len(f.data)) {
		return 0, io.EOF
	}
	n := copy(p, f.data[f.pos:])
	f.pos += int64(n)
	return n, nil
}

func (f *seekCountingFile) Seek(offset int64, whence int) (int64, error) {
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

func (f *seekCountingFile) Close() error                             { return nil }
func (f *seekCountingFile) ReadAt(p []byte, off int64) (int, error)  { return 0, nil }
func (f *seekCountingFile) Write(p []byte) (int, error)              { return 0, nil }
func (f *seekCountingFile) WriteAt(p []byte, off int64) (int, error) { return 0, nil }
func (f *seekCountingFile) Name() string                             { return "seekcounting" }
func (f *seekCountingFile) Readdir(count int) ([]os.FileInfo, error) { return nil, nil }
func (f *seekCountingFile) Readdirnames(n int) ([]string, error)     { return nil, nil }
func (f *seekCountingFile) Stat() (os.FileInfo, error)               { return nil, nil }
func (f *seekCountingFile) Sync() error                              { return nil }
func (f *seekCountingFile) Truncate(size int64) error                { return nil }
func (f *seekCountingFile) WriteString(s string) (int, error)        { return 0, nil }

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
