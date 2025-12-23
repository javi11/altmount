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
