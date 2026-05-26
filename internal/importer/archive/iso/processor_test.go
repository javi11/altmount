package iso

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestAnalyzeISO_HonorsTimeout verifies the hard per-ISO deadline added by
// the IsoAnalyzeTimeoutSeconds config knob. A 1ns analyseTimeout must
// trip the context.WithTimeout, hit the fail-fast ctx.Err() check, and
// return a DeadlineExceeded-wrapped error within a few ms — well before
// any NNTP read could be attempted.
//
// Passing a nil pool.Manager is deliberate: if the timeout check fails
// to fire, NewISOReadSeeker would dereference it and crash, making the
// regression unmissable.
func TestAnalyzeISO_HonorsTimeout(t *testing.T) {
	t.Parallel()

	src := ISOSource{Filename: "stuck.iso", Size: 1 << 30}

	start := time.Now()
	_, err := AnalyzeISO(
		context.Background(),
		src,
		nil, // pool.Manager — must NOT be reached
		0,
		0,
		1*time.Nanosecond, // analyzeTimeout
		nil,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from past-deadline AnalyzeISO, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected error wrapping context.DeadlineExceeded, got: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("AnalyzeISO took %v with a 1ns timeout — fail-fast ctx check is not firing", elapsed)
	}
}

// TestAnalyzeISO_HonorsTimeout_PreCanceled covers the case where the
// caller's context is already canceled before AnalyzeISO is invoked.
// With analyzeTimeout==0 (cap disabled), the function still needs to
// surface the parent's cancellation without touching the pool.
func TestAnalyzeISO_HonorsTimeout_PreCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	src := ISOSource{Filename: "stuck.iso", Size: 1 << 30}

	start := time.Now()
	_, err := AnalyzeISO(
		ctx,
		src,
		nil,
		0,
		0,
		0, // analyzeTimeout=0 → cap disabled, parent ctx still canceled
		nil,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error from pre-canceled AnalyzeISO, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected error wrapping context.Canceled, got: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Fatalf("AnalyzeISO took %v with pre-canceled ctx — fail-fast ctx check is not firing", elapsed)
	}
}
