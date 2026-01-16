package usenet

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// TestSegmentWriter_WriteAfterClose verifies that writes after close return io.ErrClosedPipe
func TestSegmentWriter_WriteAfterClose(t *testing.T) {
	t.Parallel()

	// Create a buffered segment
	seg := newSegment("test-segment", 0, 100, 101, nil)

	// Get writer reference
	w := seg.Writer()

	// Close the segment
	if err := seg.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Attempt to write after close
	_, err := w.Write([]byte("test data"))
	if err == nil {
		t.Fatal("Expected error when writing after close, got nil")
	}

	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Expected io.ErrClosedPipe, got: %v", err)
	}
}

// TestSegmentWriter_ConcurrentWriteAndClose tests race condition between write and close
func TestSegmentWriter_ConcurrentWriteAndClose(t *testing.T) {
	t.Parallel()

	// Run this test multiple times to increase chance of catching race
	for i := 0; i < 10; i++ {
		seg := newSegment("test-segment", 0, 1024*100, 1024*101, nil)

		w := seg.Writer()
		var wg sync.WaitGroup
		writeErr := make(chan error, 1)

		// Start goroutine that writes
		wg.Add(1)
		go func() {
			defer wg.Done()
			data := make([]byte, 1024)
			for j := 0; j < 100; j++ {
				_, err := w.Write(data)
				if err != nil {
					writeErr <- err
					return
				}
				time.Sleep(time.Microsecond) // Small delay to increase race likelihood
			}
		}()

		// Close segment from main goroutine during write
		time.Sleep(time.Millisecond)
		if err := seg.Close(); err != nil {
			t.Errorf("Close() failed: %v", err)
		}

		wg.Wait()
		close(writeErr)

		// The writer should either succeed or get io.ErrClosedPipe
		// The important thing is no panic occurred
		if err := <-writeErr; err != nil && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Expected nil or io.ErrClosedPipe, got: %v", err)
		}
	}
}

// TestSegmentClose_Idempotent verifies that calling Close() multiple times is safe
func TestSegmentClose_Idempotent(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	// Close multiple times
	for i := 0; i < 5; i++ {
		if err := seg.Close(); err != nil {
			t.Errorf("Close() call %d failed: %v", i+1, err)
		}
	}

	// Verify closed flag is set
	seg.mx.Lock()
	if !seg.closed {
		t.Error("Expected segment to be marked as closed")
	}
	seg.mx.Unlock()
}

// TestSafeWriter_ReturnsErrorWhenClosed verifies Writer() returns safe writer even after close
func TestSafeWriter_ReturnsErrorWhenClosed(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	// Close first
	if err := seg.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Get writer after close
	w := seg.Writer()
	if w == nil {
		t.Fatal("Writer() returned nil")
	}

	// Attempt to write - should get clean error
	_, err := w.Write([]byte("test data"))
	if err == nil {
		t.Fatal("Expected error when writing to closed segment, got nil")
	}

	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Expected io.ErrClosedPipe, got: %v", err)
	}
}

// TestSegmentWriter_ConcurrentWrites tests multiple concurrent writers with close
func TestSegmentWriter_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 1024*100, 1024*101, nil)

	w := seg.Writer()
	var wg sync.WaitGroup
	numWriters := 10
	errChan := make(chan error, numWriters)

	// Start multiple concurrent writers
	for i := 0; i < numWriters; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			data := make([]byte, 100)
			for j := 0; j < 50; j++ {
				_, err := w.Write(data)
				if err != nil {
					errChan <- err
					return
				}
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// Close segment during concurrent writes
	time.Sleep(5 * time.Millisecond)
	if err := seg.Close(); err != nil {
		t.Errorf("Close() failed: %v", err)
	}

	wg.Wait()
	close(errChan)

	// All errors should be io.ErrClosedPipe
	for err := range errChan {
		if !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("Expected io.ErrClosedPipe, got: %v", err)
		}
	}
}

// TestSegmentWriter_NilBuffer verifies handling of nil buffer on closed segment
// With lazy allocation, Writer() allocates buffer on first call, so we test
// the case where segment is closed (buffer remains nil).
func TestSegmentWriter_NilBuffer(t *testing.T) {
	t.Parallel()

	seg := &segment{
		Id:     "test-segment",
		buffer: nil, // Nil buffer
		closed: true, // Mark as closed so buffer won't be allocated
		ready:  make(chan struct{}),
	}

	w := seg.Writer()
	_, err := w.Write([]byte("test"))
	if err == nil {
		t.Fatal("Expected error when writing to nil buffer, got nil")
	}

	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Expected io.ErrClosedPipe for nil buffer, got: %v", err)
	}
}

// TestSegmentClose_NilSegment verifies Close() handles nil segment safely
func TestSegmentClose_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	if err := seg.Close(); err != nil {
		t.Errorf("Close() on nil segment should return nil, got: %v", err)
	}
}

// TestSegmentWriter_RaceDetection is designed to be run with -race flag
func TestSegmentWriter_RaceDetection(t *testing.T) {
	t.Parallel()

	// This test is specifically designed to catch data races
	// Run with: go test -race -run TestSegmentWriter_RaceDetection
	for iteration := 0; iteration < 20; iteration++ {
		seg := newSegment("test-segment", 0, 1024, 1025, nil)

		w := seg.Writer()
		var wg sync.WaitGroup

		// Multiple goroutines accessing Writer() and Close() concurrently
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = seg.Writer()
			}()
		}

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = w.Write([]byte("test"))
			}()
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(time.Microsecond)
			_ = seg.Close()
		}()

		wg.Wait()
	}
}

// TestSegment_CloseWithError_StoresError verifies that CloseWithError stores the error
func TestSegment_CloseWithError_StoresError(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	testErr := errors.New("article not found in providers")

	// Get writer and close with error
	sw := seg.Writer()
	safeW, ok := sw.(interface{ CloseWithError(error) error })
	if !ok {
		t.Fatal("Writer does not implement CloseWithError")
	}

	err := safeW.CloseWithError(testErr)
	if err != nil {
		t.Fatalf("CloseWithError failed: %v", err)
	}

	// Check that error is stored
	storedErr := seg.GetDownloadError()
	if storedErr == nil {
		t.Fatal("Expected download error to be stored, got nil")
	}
	if !errors.Is(storedErr, testErr) {
		t.Errorf("Expected error %v, got %v", testErr, storedErr)
	}
}

// TestSegment_GetReader_PropagatesStoredError verifies that stored errors are returned on Read
func TestSegment_GetReader_PropagatesStoredError(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	testErr := errors.New("article not found in providers")

	// Close with error before reading
	sw := seg.Writer()
	safeW, ok := sw.(interface{ CloseWithError(error) error })
	if !ok {
		t.Fatal("Writer does not implement CloseWithError")
	}
	_ = safeW.CloseWithError(testErr)

	// GetReader should work (download signals complete via CloseWithError)
	r := seg.GetReader()
	if r == nil {
		t.Fatal("GetReader returned nil")
	}

	// Read should return the stored error
	buf := make([]byte, 10)
	_, err := r.Read(buf)
	if err == nil {
		t.Fatal("Expected error on read, got nil")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("Expected error %v, got %v", testErr, err)
	}
}

// TestSegment_SetDownloadError_FirstWriteWins verifies first-write-wins semantics
func TestSegment_SetDownloadError_FirstWriteWins(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)
	defer seg.Close()

	firstErr := errors.New("first error")
	secondErr := errors.New("second error")

	// Set first error
	seg.SetDownloadError(firstErr)

	// Try to set second error
	seg.SetDownloadError(secondErr)

	// Should still have first error
	storedErr := seg.GetDownloadError()
	if !errors.Is(storedErr, firstErr) {
		t.Errorf("Expected first error to be preserved, got %v", storedErr)
	}
}

// TestSegment_GetDownloadError_NilSegment verifies nil segment handling
func TestSegment_GetDownloadError_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	if seg.GetDownloadError() != nil {
		t.Error("Expected nil error for nil segment")
	}
}

// TestSegment_SetDownloadError_NilSegment verifies nil segment handling
func TestSegment_SetDownloadError_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	// Should not panic
	seg.SetDownloadError(errors.New("test error"))
}

// TestSegment_ErrorPropagation_ConcurrentAccess tests thread safety of error propagation
func TestSegment_ErrorPropagation_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 10; iteration++ {
		seg := newSegment("test-segment", 0, 100, 101, nil)

		testErr := errors.New("concurrent error")
		var wg sync.WaitGroup

		// Multiple goroutines setting and getting error
		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				seg.SetDownloadError(testErr)
			}()
		}

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_ = seg.GetDownloadError()
			}()
		}

		wg.Wait()

		// Error should be set
		if seg.GetDownloadError() == nil {
			t.Error("Expected error to be set after concurrent access")
		}

		_ = seg.Close()
	}
}

// TestSegment_BufferedWrite_NoBlocking verifies writes don't block
func TestSegment_BufferedWrite_NoBlocking(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 1024*1024, 1024*1024+1, nil)

	w := seg.Writer()

	// Write 1MB of data - should not block even without a reader
	data := make([]byte, 1024*1024)
	done := make(chan struct{})

	go func() {
		_, err := w.Write(data)
		if err != nil {
			t.Errorf("Write failed: %v", err)
		}
		close(done)
	}()

	// Should complete quickly without blocking
	select {
	case <-done:
		// Success - write completed without blocking
	case <-time.After(time.Second):
		t.Fatal("Write blocked - this indicates the old io.Pipe behavior")
	}

	// Close the writer to signal completion
	if closer, ok := w.(io.Closer); ok {
		closer.Close()
	}

	seg.Close()
}

// TestSegment_ReadAfterWriteComplete verifies read works after write completes
func TestSegment_ReadAfterWriteComplete(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 10, 11, nil)

	// Write data
	w := seg.Writer()
	testData := []byte("hello world")
	n, err := w.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	if n != len(testData) {
		t.Fatalf("Expected to write %d bytes, wrote %d", len(testData), n)
	}

	// Close writer to signal download complete
	if closer, ok := w.(io.Closer); ok {
		closer.Close()
	}

	// Read data
	r := seg.GetReader()
	buf := make([]byte, 100)
	n, err = r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	// Should read "hello world" (first 11 bytes, Start=0, End=10)
	if n != 11 {
		t.Errorf("Expected to read 11 bytes, read %d", n)
	}
	if !bytes.Equal(buf[:n], testData[:11]) {
		t.Errorf("Expected %q, got %q", testData[:11], buf[:n])
	}

	seg.Close()
}

// TestSegment_ReadWithOffset verifies reading with Start offset works
func TestSegment_ReadWithOffset(t *testing.T) {
	t.Parallel()

	// Create segment with offset: Start=5, End=9 (read bytes 5-9, length=5)
	seg := newSegment("test-segment", 5, 9, 20, nil)

	// Write data: "01234567890123456789" (20 bytes)
	w := seg.Writer()
	testData := []byte("01234567890123456789")
	_, err := w.Write(testData)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// Close writer to signal download complete
	if closer, ok := w.(io.Closer); ok {
		closer.Close()
	}

	// Read data - should get bytes 5-9: "56789"
	r := seg.GetReader()
	buf := make([]byte, 100)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read failed: %v", err)
	}

	expected := []byte("56789")
	if n != len(expected) {
		t.Errorf("Expected to read %d bytes, read %d", len(expected), n)
	}
	if !bytes.Equal(buf[:n], expected) {
		t.Errorf("Expected %q, got %q", expected, buf[:n])
	}

	seg.Close()
}

// TestSegment_BufferPoolReuse verifies buffer pool is used
func TestSegment_BufferPoolReuse(t *testing.T) {
	t.Parallel()

	// Create and close multiple segments to exercise pool
	for i := 0; i < 100; i++ {
		seg := newSegment("test-segment", 0, 100, 101, nil)

		w := seg.Writer()
		w.Write([]byte("test data"))

		if closer, ok := w.(io.Closer); ok {
			closer.Close()
		}

		seg.Close()
	}

	// If we got here without panic or memory issues, the pool is working
}

// TestSegment_LazyBufferAllocation verifies buffer is allocated lazily
func TestSegment_LazyBufferAllocation(t *testing.T) {
	t.Parallel()

	// Create segment - buffer should NOT be allocated yet
	seg := newSegment("test-segment", 0, 100, 101, nil)

	// Verify buffer is nil before Writer() is called
	seg.mx.Lock()
	if seg.buffer != nil {
		t.Error("Expected buffer to be nil before Writer() is called")
	}
	seg.mx.Unlock()

	// Call Writer() - this should allocate the buffer
	w := seg.Writer()

	// Verify buffer is now allocated
	seg.mx.Lock()
	if seg.buffer == nil {
		t.Error("Expected buffer to be allocated after Writer() is called")
	}
	bufferPtr := seg.buffer
	seg.mx.Unlock()

	// Multiple Writer() calls should not reallocate the buffer
	_ = seg.Writer()
	_ = seg.Writer()

	seg.mx.Lock()
	if seg.buffer != bufferPtr {
		t.Error("Expected buffer to remain the same after multiple Writer() calls")
	}
	seg.mx.Unlock()

	// Write should still work
	_, err := w.Write([]byte("test data"))
	if err != nil {
		t.Errorf("Write failed: %v", err)
	}

	seg.Close()
}

// TestSegment_LazyAllocation_ClosedSegment verifies buffer is not allocated on closed segment
func TestSegment_LazyAllocation_ClosedSegment(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	// Close segment first
	if err := seg.Close(); err != nil {
		t.Fatalf("Close() failed: %v", err)
	}

	// Call Writer() on closed segment - should not allocate buffer
	w := seg.Writer()

	seg.mx.Lock()
	if seg.buffer != nil {
		t.Error("Expected buffer to remain nil for closed segment")
	}
	seg.mx.Unlock()

	// Write should fail with ErrClosedPipe
	_, err := w.Write([]byte("test"))
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Expected io.ErrClosedPipe, got: %v", err)
	}
}

// TestSegment_LazyAllocation_ManySegments verifies memory efficiency with many segments
func TestSegment_LazyAllocation_ManySegments(t *testing.T) {
	t.Parallel()

	// Create many segments without calling Writer()
	// This simulates GetSegmentsInRange creating segments for a large file
	segments := make([]*segment, 1000)
	for i := 0; i < 1000; i++ {
		segments[i] = newSegment("test-segment", 0, 768*1024-1, 768*1024, nil)
	}

	// Verify no buffers are allocated yet
	for i, seg := range segments {
		seg.mx.Lock()
		if seg.buffer != nil {
			t.Errorf("Segment %d: expected buffer to be nil", i)
		}
		seg.mx.Unlock()
	}

	// Only allocate buffers for a few segments (simulating download manager)
	for i := 0; i < 10; i++ {
		_ = segments[i].Writer()
	}

	// Verify only 10 buffers are allocated
	allocatedCount := 0
	for _, seg := range segments {
		seg.mx.Lock()
		if seg.buffer != nil {
			allocatedCount++
		}
		seg.mx.Unlock()
	}

	if allocatedCount != 10 {
		t.Errorf("Expected 10 buffers allocated, got %d", allocatedCount)
	}

	// Clean up
	for _, seg := range segments {
		seg.Close()
	}
}
