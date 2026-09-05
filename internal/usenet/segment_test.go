package usenet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

// TestSegment_SetData_ThenGetReader verifies basic data flow: SetData -> GetReader -> Read
func TestSegment_SetData_ThenGetReader(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 9, 10, nil, 0)

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
	seg := newSegment("test-segment", 3, 6, 10, nil, 0)

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

	seg := newSegment("test-segment", 0, 100, 101, nil, 0)
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

	seg := newSegment("test-segment", 0, 100, 101, nil, 0)

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

	seg := newSegment("test-segment", 0, 100, 101, nil, 0)

	for i := range 5 {
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

	seg := newSegment("test-segment", 0, 100, 101, nil, 0)

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

	seg := newSegment("test-segment", 0, 100, 101, nil, 0)
	seg.Release()

	// Should not panic
	seg.SetData([]byte("data"))

	if seg.DataLen() != 0 {
		t.Error("Expected no data after Release")
	}
}

// TestSegment_DataLen(t *testing.T) verifies DataLen returns correct values
func TestSegment_DataLen(t *testing.T) {
	t.Parallel()

	seg := newSegment("test-segment", 0, 9, 10, nil, 0)

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

	for range 20 {
		seg := newSegment("test-segment", 0, 9, 10, nil, 0)

		var wg sync.WaitGroup

		// One reader goroutine (matches real usage)
		wg.Go(func() {
			r := seg.GetReader()
			buf := make([]byte, 10)
			_, _ = r.Read(buf)
		})

		// Set data from another goroutine
		wg.Go(func() {
			seg.SetData([]byte("0123456789"))
		})

		wg.Wait()
	}
}

// TestSegment_ConcurrentSetErrorAndGetReader tests concurrent error + read access
func TestSegment_ConcurrentSetErrorAndGetReader(t *testing.T) {
	t.Parallel()

	for range 20 {
		seg := newSegment("test-segment", 0, 100, 101, nil, 0)
		testErr := errors.New("concurrent error")

		var wg sync.WaitGroup

		for range 5 {
			wg.Go(func() {
				seg.SetError(testErr)
			})
		}

		for range 5 {
			wg.Go(func() {
				_ = seg.GetDownloadError()
			})
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

	for range 20 {
		seg := newSegment("test-segment", 0, 9, 10, nil, 0)

		var wg sync.WaitGroup

		// One reader goroutine (matches real usage)
		wg.Go(func() {
			r := seg.GetReader()
			buf := make([]byte, 10)
			_, _ = r.Read(buf)
		})

		// Release from another goroutine
		wg.Go(func() {
			seg.Release()
		})

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
	for i := range numSegments {
		segments[i] = newSegment("segment-"+string(rune('0'+i)), 0, 100, 101, nil, 0)
	}

	// Pre-release segment 2 to simulate already-closed state
	segments[2].Release()

	sr := &segmentRange{
		segments: segments,
		current:  0,
	}

	_ = sr.Clear()

	for i := range numSegments {
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
	for i := range numSegments {
		segments[i] = newSegment("segment", 0, 100, 101, nil, 0)
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
		newSegment("s1", 0, 100, 101, nil, 0),
		nil, // nil segment
		newSegment("s2", 0, 100, 101, nil, 0),
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

	for i := range numSegments {
		segments[i] = newSegment("segment", 0, 100, 101, nil, 0)
	}

	sr := &segmentRange{
		segments: segments,
	}

	var wg sync.WaitGroup

	for range 3 {
		wg.Go(func() {
			_ = sr.Clear()
		})
	}

	wg.Wait()
}

// TestSegmentRange_GetSegment_ConcurrentClear reproduces issue #870: GetSegment
// validated the index under RLock, then re-acquired the write lock for lazy
// creation and indexed r.segments without re-checking bounds. A concurrent
// Clear() nil-ing the slice in that window panicked with index out of range.
func TestSegmentRange_GetSegment_ConcurrentClear(t *testing.T) {
	t.Parallel()

	loader := &mockLoader{
		segments: []Segment{{Id: "seg", Start: 0, End: 99, Size: 100}},
		groups:   [][]string{nil},
	}

	for range 20000 {
		sr := &segmentRange{
			start:    0,
			end:      99,
			segments: make([]*segment, 1),
			ctx:      context.Background(),
			loader:   loader,
		}

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			seg, err := sr.GetSegment(0)
			if err == nil && seg == nil {
				t.Error("GetSegment returned nil segment without error")
			}
		}()
		go func() {
			defer wg.Done()
			_ = sr.Clear()
		}()
		wg.Wait()
	}
}

// BenchmarkClear benchmarks the Clear operation
func BenchmarkClear(b *testing.B) {
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		segments := make([]*segment, 100)
		for j := range 100 {
			segments[j] = newSegment("segment", 0, 100, 101, nil, 0)
		}
		sr := &segmentRange{segments: segments}
		b.StartTimer()

		_ = sr.Clear()
	}
}

func TestSegmentServesBytesBeforeFinish(t *testing.T) {
	s := newSegment("id", 0, 9, 10, nil, 0)
	w := s.attemptWriter()
	_, _ = w.Write([]byte{1, 2, 3, 4})

	r := s.GetReaderContext(context.Background())
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	if err != nil || n != 3 || !bytes.Equal(buf, []byte{1, 2, 3}) {
		t.Fatalf("read before finish: n=%d err=%v buf=%v", n, err, buf)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rest := make([]byte, 10)
		total := 0
		for total < 7 {
			nn, err := r.Read(rest[total:])
			total += nn
			if err != nil {
				t.Errorf("tail read: %v", err)
				return
			}
		}
		if !bytes.Equal(rest[:7], []byte{4, 5, 6, 7, 8, 9, 10}) {
			t.Errorf("tail = %v", rest[:7])
		}
	}()
	time.Sleep(20 * time.Millisecond)
	_, _ = w.Write([]byte{5, 6, 7, 8, 9, 10})
	s.finish(w)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader never got the tail")
	}
	if _, err := r.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("after full range want EOF, got %v", err)
	}
}

func TestSegmentSecondAttemptDoesNotRewind(t *testing.T) {
	s := newSegment("id", 0, 7, 8, nil, 0)
	w1 := s.attemptWriter()
	_, _ = w1.Write([]byte{1, 2, 3, 4})
	if s.published() != 4 {
		t.Fatalf("published = %d", s.published())
	}
	w2 := s.attemptWriter()
	_, _ = w2.Write([]byte{1, 2})
	if s.published() != 4 {
		t.Fatalf("a shorter second attempt must not rewind: %d", s.published())
	}
	_, _ = w2.Write([]byte{3, 4, 5, 6})
	if s.published() != 6 {
		t.Fatalf("second attempt past the watermark must publish: %d", s.published())
	}
	_, _ = w2.Write([]byte{7, 8})
	s.finish(w2)
	got, err := io.ReadAll(s.GetReaderContext(context.Background()))
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("got %v err %v", got, err)
	}
	if !bytes.Equal(w2.bytes(), got) {
		t.Fatal("bytes() must return the finished buffer")
	}
}

func TestSegmentTrimmedStartWaitsOnlyForItsFirstByte(t *testing.T) {
	s := newSegment("id", 5, 9, 10, nil, 0)
	w := s.attemptWriter()
	_, _ = w.Write([]byte{0, 1, 2, 3, 4, 5, 6})
	r := s.GetReaderContext(context.Background())
	buf := make([]byte, 2)
	n, err := r.Read(buf)
	if err != nil || n != 2 || !bytes.Equal(buf, []byte{5, 6}) {
		t.Fatalf("n=%d err=%v buf=%v", n, err, buf)
	}
}

func TestSegmentReleaseAndCancelUnblockProgressiveReader(t *testing.T) {
	s := newSegment("id", 0, 9, 10, nil, 0)
	w := s.attemptWriter()
	_, _ = w.Write([]byte{1})
	r := s.GetReaderContext(context.Background())
	_, _ = r.Read(make([]byte, 1))
	errc := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 1)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	s.Release()
	if err := <-errc; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("release must unblock with ErrClosedPipe, got %v", err)
	}

	s2 := newSegment("id2", 0, 9, 10, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	r2 := s2.GetReaderContext(ctx)
	go func() { _, err := r2.Read(make([]byte, 1)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel must unblock, got %v", err)
	}
}

func TestSegmentSetDataStillWorks(t *testing.T) {
	s := newSegment("id", 2, 5, 6, nil, 0)
	s.SetData([]byte{0, 1, 2, 3, 4, 5})
	got, err := io.ReadAll(s.GetReader())
	if err != nil || !bytes.Equal(got, []byte{2, 3, 4, 5}) {
		t.Fatalf("got %v err %v", got, err)
	}
	if s.DataLen() != 6 {
		t.Fatalf("DataLen = %d", s.DataLen())
	}
}
