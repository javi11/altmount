package usenet

import (
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// TestSegmentWriter_WriteAfterClose verifies that writes after close return io.ErrClosedPipe
func TestSegmentWriter_WriteAfterClose(t *testing.T) {
	t.Parallel()

	// Create a segment with a pipe
	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		reader: reader,
		writer: writer,
	}

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
		reader, writer := io.Pipe()
		seg := &segment{
			Id:     "test-segment",
			reader: reader,
			writer: writer,
		}

		w := seg.Writer()
		var wg sync.WaitGroup
		writeErr := make(chan error, 1)

		// Start goroutine that writes slowly
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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		reader: reader,
		writer: writer,
	}

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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		reader: reader,
		writer: writer,
	}

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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		reader: reader,
		writer: writer,
	}

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

// TestSegmentWriter_NilWriter verifies handling of nil writer
func TestSegmentWriter_NilWriter(t *testing.T) {
	t.Parallel()

	seg := &segment{
		Id:     "test-segment",
		writer: nil, // Nil writer
	}

	w := seg.Writer()
	_, err := w.Write([]byte("test"))
	if err == nil {
		t.Fatal("Expected error when writing to nil writer, got nil")
	}

	if !errors.Is(err, io.ErrClosedPipe) {
		t.Errorf("Expected io.ErrClosedPipe for nil writer, got: %v", err)
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
		reader, writer := io.Pipe()
		seg := &segment{
			Id:     "test-segment",
			reader: reader,
			writer: writer,
		}

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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		Start:  0,
		End:    100,
		reader: reader,
		writer: writer,
	}

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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:          "test-segment",
		Start:       0,
		End:         100,
		SegmentSize: 100,
		reader:      reader,
		writer:      writer,
	}

	testErr := errors.New("article not found in providers")

	// Close with error before reading
	sw := seg.Writer()
	safeW, ok := sw.(interface{ CloseWithError(error) error })
	if !ok {
		t.Fatal("Writer does not implement CloseWithError")
	}
	_ = safeW.CloseWithError(testErr)

	// GetReader should work
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

	reader, writer := io.Pipe()
	seg := &segment{
		Id:     "test-segment",
		reader: reader,
		writer: writer,
	}
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

// TestSegment_ErrorAwareReader_PropagatesErrorBeforeRead verifies error check before read
func TestSegment_ErrorAwareReader_PropagatesErrorBeforeRead(t *testing.T) {
	t.Parallel()

	reader, writer := io.Pipe()
	seg := &segment{
		Id:          "test-segment",
		Start:       0,
		End:         100,
		SegmentSize: 100,
		reader:      reader,
		writer:      writer,
	}

	// Set error before calling GetReader
	testErr := errors.New("download failed")
	seg.SetDownloadError(testErr)

	// GetReader and attempt to read
	r := seg.GetReader()
	buf := make([]byte, 10)
	_, err := r.Read(buf)

	if !errors.Is(err, testErr) {
		t.Errorf("Expected error %v, got %v", testErr, err)
	}
}

// TestSegment_ErrorPropagation_ConcurrentAccess tests thread safety of error propagation
func TestSegment_ErrorPropagation_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 10; iteration++ {
		reader, writer := io.Pipe()
		seg := &segment{
			Id:          "test-segment",
			Start:       0,
			End:         100,
			SegmentSize: 100,
			reader:      reader,
			writer:      writer,
		}

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
