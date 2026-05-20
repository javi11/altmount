package usenet

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// Package: invariant pinning for UsenetReader prefetch and retry behavior.
//
// Each test in this file documents a property that the streaming pipeline
// MUST honor for any combination of provider speed, segment count, and
// reader pace. The tests use a fake nntppool client whose in-flight counter
// records the high-water mark of concurrent BodyPriority calls — that
// metric is the contract these tests pin.
//
// These tests live in `package usenet` rather than an external _test
// package because segmentRange and segment are unexported; constructing
// them directly is the only way to exercise UsenetReader without dragging
// the metadata layer in.

// TestPrefetch_RespectsMaxPrefetchUnderSteadyRead pins the per-reader
// invariant: with maxPrefetch=N, the downloadManager must never schedule
// more than N segments concurrently ahead of the current read position.
//
// Method: 50 segments, maxPrefetch=4, a slow provider (30ms per segment)
// and a fast reader. We let the reader drain everything, then assert the
// fake pool's MaxInFlight high-water mark stays <= maxPrefetch.
//
// This test should pass on current code — it documents existing correct
// behavior so future refactors of downloadManager don't regress it.
func TestPrefetch_RespectsMaxPrefetchUnderSteadyRead(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 50
		segSize     = 32
		maxPrefetch = 4
		segLatency  = 30 * time.Millisecond
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < segCount; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Latency: segLatency,
			Bytes:   segments.Payload(i, segSize),
		})
	}

	rg := buildEagerRange(ctx, t, segCount, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, maxPrefetch)
	ur.Start()

	if _, err := io.ReadAll(ur); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}

	fakepool.AssertMaxInFlightLE(t, fp, int32(maxPrefetch))
	if got := fp.BodyPriorityCalls(); got != int64(segCount) {
		t.Errorf("BodyPriorityCalls = %d, want %d (one per segment, no retries)",
			got, segCount)
	}
}

// TestPrefetch_DoesNotExceedMaxPrefetchOnSlowPool pins the same invariant
// in the worst-case shape: a very slow provider (100ms) and a reader that
// blocks for nothing. If downloadManager were to schedule eagerly without
// honoring maxPrefetch when the pool itself is the bottleneck, MaxInFlight
// would rise above the cap — that is the storm condition.
//
// Should pass on current code; protects against a regression that
// "schedules more aggressively when downloads look slow".
func TestPrefetch_DoesNotExceedMaxPrefetchOnSlowPool(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 20
		segSize     = 16
		maxPrefetch = 3
		segLatency  = 100 * time.Millisecond
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fp := fakepool.New()
	for i := 0; i < segCount; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Latency: segLatency,
			Bytes:   segments.Payload(i, segSize),
		})
	}

	rg := buildEagerRange(ctx, t, segCount, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, maxPrefetch)
	ur.Start()

	if _, err := io.ReadAll(ur); err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	fakepool.AssertMaxInFlightLE(t, fp, int32(maxPrefetch))
}
