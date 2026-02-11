package usenet

import (
	"errors"
	"io"
	"sync"
	"testing"
)

// TestSegment_SetData_ThenGetReader verifies basic data flow: SetData -> GetReader -> Read
func TestSegment_SetData_ThenGetReader(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 9, 10, nil)

	// Set data
	seg.SetData([]byte("0123456789"))

	// Read it back
	r := seg.GetReader()
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 10 {
		t.Fatalf("Expected 10 bytes, got %d", n)
	}
	if string(buf[:n]) != "0123456789" {
		t.Fatalf("Expected '0123456789', got '%s'", string(buf[:n]))
	}
}

// TestSegment_SetData_WithOffset verifies that Start offset is applied correctly
func TestSegment_SetData_WithOffset(t *testing.T) {
	t.Parallel()

	// Segment reads bytes [3, 6] from a 10-byte segment
	seg := newSegment("test-segment", 3, 6, 10, nil)

	seg.SetData([]byte("0123456789"))

	r := seg.GetReader()
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("Read() failed: %v", err)
	}
	if n != 4 {
		t.Fatalf("Expected 4 bytes, got %d", n)
	}
	if string(buf[:n]) != "3456" {
		t.Fatalf("Expected '3456', got '%s'", string(buf[:n]))
	}
}

// TestSegment_SetError_PropagatesOnRead verifies that SetError makes GetReader return the error
func TestSegment_SetError_PropagatesOnRead(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)
	testErr := errors.New("article not found in providers")

	seg.SetError(testErr)

	r := seg.GetReader()
	buf := make([]byte, 10)
	_, err := r.Read(buf)
	if err == nil {
		t.Fatal("Expected error on read, got nil")
	}
	if !errors.Is(err, testErr) {
		t.Errorf("Expected error %v, got %v", testErr, err)
	}
}

// TestSegment_SetError_FirstWriteWins verifies first-write-wins semantics
func TestSegment_SetError_FirstWriteWins(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	firstErr := errors.New("first error")
	secondErr := errors.New("second error")

	seg.SetError(firstErr)
	seg.SetError(secondErr)

	storedErr := seg.GetDownloadError()
	if !errors.Is(storedErr, firstErr) {
		t.Errorf("Expected first error to be preserved, got %v", storedErr)
	}
}

// TestSegment_Close_Idempotent verifies that calling Close() multiple times is safe
func TestSegment_Close_Idempotent(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	for i := 0; i < 5; i++ {
		if err := seg.Close(); err != nil {
			t.Errorf("Close() call %d failed: %v", i+1, err)
		}
	}

	seg.mx.Lock()
	if !seg.released {
		t.Error("Expected segment to be marked as released")
	}
	seg.mx.Unlock()
}

// TestSegment_Release_UnblocksGetReader verifies that Release unblocks a waiting GetReader
func TestSegment_Release_UnblocksGetReader(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		r := seg.GetReader()
		buf := make([]byte, 10)
		_, err := r.Read(buf)
		if err == nil {
			t.Error("Expected error after release, got nil")
		}
	}()

	// Release should unblock the GetReader
	seg.Release()
	<-done
}

// TestSegment_SetData_AfterRelease verifies that SetData after Release is a no-op
func TestSegment_SetData_AfterRelease(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 100, 101, nil)
	seg.Release()

	// Should not panic
	seg.SetData([]byte("data"))

	seg.mx.Lock()
	if seg.data != nil {
		t.Error("Expected data to be nil after Release")
	}
	seg.mx.Unlock()
}

// TestSegment_DataLen(t *testing.T) verifies DataLen returns correct values
func TestSegment_DataLen(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 9, 10, nil)

	// Before data is set
	if seg.DataLen() != 0 {
		t.Errorf("Expected DataLen 0 before SetData, got %d", seg.DataLen())
	}

	seg.SetData([]byte("0123456789"))

	if seg.DataLen() != 10 {
		t.Errorf("Expected DataLen 10 after SetData, got %d", seg.DataLen())
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

// TestSegment_SetError_NilSegment verifies nil segment handling
func TestSegment_SetError_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	// Should not panic
	seg.SetError(errors.New("test error"))
}

// TestSegment_SetData_NilSegment verifies nil segment handling
func TestSegment_SetData_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	// Should not panic
	seg.SetData([]byte("data"))
}

// TestSegment_Close_NilSegment verifies Close() handles nil segment safely
func TestSegment_Close_NilSegment(t *testing.T) {
	t.Parallel()

	var seg *segment
	if err := seg.Close(); err != nil {
		t.Errorf("Close() on nil segment should return nil, got: %v", err)
	}
}

// TestSegment_ConcurrentSetDataAndGetReader tests that SetData and GetReader
// don't race. Only one goroutine reads (matching real usage in UsenetReader.Read).
func TestSegment_ConcurrentSetDataAndGetReader(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 20; iteration++ {
		seg := newSegment("test-segment", 0, 9, 10, nil)

		var wg sync.WaitGroup

		// One reader goroutine (matches real usage)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := seg.GetReader()
			buf := make([]byte, 10)
			_, _ = r.Read(buf)
		}()

		// Set data from another goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			seg.SetData([]byte("0123456789"))
		}()

		wg.Wait()
	}
}

// TestSegment_ConcurrentSetErrorAndGetReader tests concurrent error + read access
func TestSegment_ConcurrentSetErrorAndGetReader(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 20; iteration++ {
		seg := newSegment("test-segment", 0, 100, 101, nil)
		testErr := errors.New("concurrent error")

		var wg sync.WaitGroup

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				seg.SetError(testErr)
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

		if seg.GetDownloadError() == nil {
			t.Error("Expected error to be set after concurrent access")
		}
	}
}

// TestSegment_ConcurrentReleaseAndGetReader tests race between release and read
func TestSegment_ConcurrentReleaseAndGetReader(t *testing.T) {
	t.Parallel()

	for iteration := 0; iteration < 20; iteration++ {
		seg := newSegment("test-segment", 0, 9, 10, nil)

		var wg sync.WaitGroup

		// One reader goroutine (matches real usage)
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := seg.GetReader()
			buf := make([]byte, 10)
			_, _ = r.Read(buf)
		}()

		// Release from another goroutine
		wg.Add(1)
		go func() {
			defer wg.Done()
			seg.Release()
		}()

		wg.Wait()
	}
}

// =============================================================================
// Tests for segmentRange.Clear()
// =============================================================================

// TestSegmentRangeClear_ContinuesOnAllSegments verifies that Clear() releases ALL segments.
func TestSegmentRangeClear_ContinuesOnAllSegments(t *testing.T) {
	t.Parallel()

	const numSegments = 5

	segments := make([]*segment, numSegments)
	for i := 0; i < numSegments; i++ {
		segments[i] = newSegment("segment-"+string(rune('0'+i)), 0, 100, 101, nil)
	}

	// Pre-release segment 2 to simulate already-closed state
	segments[2].Release()

	sr := &segmentRange{
		segments: segments,
		current:  0,
	}

	_ = sr.Clear()

	for i := 0; i < numSegments; i++ {
		segments[i].mx.Lock()
		isReleased := segments[i].released
		segments[i].mx.Unlock()

		if !isReleased {
			t.Errorf("Segment %d should be released after Clear(), but released=%v", i, isReleased)
		}
	}

	if sr.segments != nil {
		t.Error("Expected segments slice to be nil after Clear()")
	}
}

// TestSegmentRangeClear_AllSegmentsReleased verifies proper release on fresh segmentRange.
func TestSegmentRangeClear_AllSegmentsReleased(t *testing.T) {
	t.Parallel()

	const numSegments = 10

	segments := make([]*segment, numSegments)
	for i := 0; i < numSegments; i++ {
		segments[i] = newSegment("segment", 0, 100, 101, nil)
	}

	sr := &segmentRange{
		segments: segments,
		current:  0,
	}

	err := sr.Clear()
	if err != nil {
		t.Logf("Clear() returned error (unexpected): %v", err)
	}

	for i, seg := range segments {
		seg.mx.Lock()
		isReleased := seg.released
		seg.mx.Unlock()

		if !isReleased {
			t.Errorf("Segment %d should be released after Clear()", i)
		}
	}

	if sr.segments != nil {
		t.Error("Expected segments slice to be nil after Clear()")
	}
}

// TestSegmentRangeClear_NilSegmentsHandled verifies that Clear() handles nil
// segments in the slice gracefully.
func TestSegmentRangeClear_NilSegmentsHandled(t *testing.T) {
	t.Parallel()

	segments := []*segment{
		newSegment("s1", 0, 100, 101, nil),
		nil, // nil segment
		newSegment("s2", 0, 100, 101, nil),
	}

	sr := &segmentRange{
		segments: segments,
	}

	err := sr.Clear()
	if err != nil {
		t.Logf("Clear() returned error: %v", err)
	}

	segments[0].mx.Lock()
	if !segments[0].released {
		t.Error("Segment 0 should be released")
	}
	segments[0].mx.Unlock()

	segments[2].mx.Lock()
	if !segments[2].released {
		t.Error("Segment 2 should be released")
	}
	segments[2].mx.Unlock()
}

// TestSegmentRangeClear_EmptyRange verifies that Clear() handles empty ranges.
func TestSegmentRangeClear_EmptyRange(t *testing.T) {
	t.Parallel()

	sr := &segmentRange{
		segments: []*segment{},
	}

	err := sr.Clear()
	if err != nil {
		t.Errorf("Clear() on empty range returned error: %v", err)
	}
}

// TestSegmentRangeClear_NilRange verifies Clear() handles nil segment slice.
func TestSegmentRangeClear_NilRange(t *testing.T) {
	t.Parallel()

	sr := &segmentRange{
		segments: nil,
	}

	err := sr.Clear()
	if err != nil {
		t.Errorf("Clear() on nil segments returned error: %v", err)
	}
}

// TestSegmentRangeClear_ConcurrentSafety tests that Clear is safe with concurrent access.
func TestSegmentRangeClear_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	const numSegments = 10
	segments := make([]*segment, numSegments)

	for i := 0; i < numSegments; i++ {
		segments[i] = newSegment("segment", 0, 100, 101, nil)
	}

	sr := &segmentRange{
		segments: segments,
	}

	var wg sync.WaitGroup

	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sr.Clear()
		}()
	}

	wg.Wait()
}

// BenchmarkClear benchmarks the Clear operation
func BenchmarkClear(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		segments := make([]*segment, 100)
		for j := 0; j < 100; j++ {
			segments[j] = newSegment("segment", 0, 100, 101, nil)
		}
		sr := &segmentRange{segments: segments}
		b.StartTimer()

		_ = sr.Clear()
	}
}
