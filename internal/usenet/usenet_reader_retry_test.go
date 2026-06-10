package usenet

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
)

// usenet_reader_retry_test.go pins the retry-policy invariants for the
// segment download path.

// TestRetry_ArticleNotFound_NoRetry pins the existing fast-fail policy:
// nntppool.ErrArticleNotFound is a permanent failure and MUST NOT trigger
// a retry. Retrying a missing article wastes provider connections for an
// answer that will never change, and is a measurable contributor to
// connection-storm conditions when whole batches of articles have expired.
//
// The downloadSegmentWithRetry path uses retry.RetryIf to short-circuit
// this error class; this test pins that exactly one BodyPriority call is
// made even though retry.Attempts is 5.
//
// Should pass on current code.
func TestRetry_ArticleNotFound_NoRetry(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 4
		segSize     = 16
		maxPrefetch = 4
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fp := fakepool.New()
	// First segment is permanently missing; subsequent segments succeed —
	// but we expect the reader to short-circuit on the first failure with
	// DataCorruptionError, so they may not be requested at all.
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Err: nntppool.ErrArticleNotFound,
	})
	for i := 1; i < segCount; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: segments.Payload(i, segSize),
		})
	}

	rg := buildEagerRange(ctx, t, segCount, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, maxPrefetch)
	ur.Start()

	_, err := io.ReadAll(ur)
	// We don't care about the exact error class here — the invariant is
	// purely on call count.
	_ = err

	// Wait briefly for any straggling prefetch goroutines (started for
	// segments 1..N before segment 0's error short-circuited the reader).
	time.Sleep(100 * time.Millisecond)

	// segment 0 must have been requested exactly once: the retry policy
	// must NOT have re-issued the BodyPriority call.
	if got := fp.PerMessageCalls(segments.MessageID(0)); got != 1 {
		t.Errorf("segment 0 issued %d BodyPriority calls, want exactly 1 (no retry on ErrArticleNotFound)", got)
	}
}

// TestRetry_ContextCancellation_StopsImmediately pins another half of the
// retry contract: when the reader's context is cancelled mid-flight, any
// pending retry loop MUST honor cancellation and stop issuing new
// BodyPriority calls.
//
// Without this guarantee, closing a stream during a flaky-provider window
// would let the retry loop keep firing requests even though the consumer
// is gone — another way connection counts spike.
//
// Should pass on current code (retry-go honors ctx via retry.Context).
func TestRetry_ContextCancellation_StopsImmediately(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 3
		segSize     = 16
		maxPrefetch = 3
	)
	parent, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fp := fakepool.New()
	// Every call fails with a retryable error, with 50ms latency. The
	// retry loop should keep trying until we cancel.
	fp.SetDefaultBehavior(fakepool.SegmentBehavior{
		Latency: 50 * time.Millisecond,
		Err:     errors.New("synthetic transient error"),
	})

	rg := buildEagerRange(parent, t, segCount, segSize)
	ur := newReaderForTest(t, parent, fp, rg, maxPrefetch)
	ur.Start()

	// Let the retry loop spin a few times.
	time.Sleep(150 * time.Millisecond)

	beforeClose := fp.BodyPriorityCalls()
	if beforeClose == 0 {
		t.Fatalf("expected at least one BodyPriority call before close; got 0")
	}

	if err := ur.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Give any in-flight call up to 200ms to finish naturally, then snapshot.
	time.Sleep(200 * time.Millisecond)
	settled := fp.BodyPriorityCalls()

	// After settling, no further calls should be issued.
	time.Sleep(200 * time.Millisecond)
	after := fp.BodyPriorityCalls()

	if after != settled {
		t.Errorf("BodyPriority calls increased after Close: settled=%d, after=%d",
			settled, after)
	}
}

// TestMissingSegment_EmitsDebugLog verifies that a DebugContext log with
// message "missing segment" is emitted when a segment permanently fails
// with ErrArticleNotFound.
func TestMissingSegment_EmitsDebugLog(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 1
		segSize     = 16
		maxPrefetch = 1
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fp := fakepool.New()
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Err: nntppool.ErrArticleNotFound,
	})

	var mu sync.Mutex
	type logRecord struct {
		msg  string
		segID string
	}
	var captured []logRecord

	handler := &captureLogHandler{
		onHandle: func(r slog.Record) {
			var segID string
			r.Attrs(func(a slog.Attr) bool {
				if a.Key == "segment_id" {
					segID = a.Value.String()
				}
				return true
			})
			mu.Lock()
			captured = append(captured, logRecord{msg: r.Message, segID: segID})
			mu.Unlock()
		},
	}

	rg := buildEagerRange(ctx, t, segCount, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, maxPrefetch)
	ur.log = slog.New(handler)
	ur.Start()

	_, _ = io.ReadAll(ur)

	// Allow prefetch goroutines to finish.
	time.Sleep(100 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	found := false
	for _, r := range captured {
		if r.msg == "missing segment" && r.segID == segments.MessageID(0) {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'missing segment' debug log for segment_id=%q, got: %+v", segments.MessageID(0), captured)
}

// captureLogHandler is a minimal slog.Handler that invokes onHandle for
// every log record. All levels are enabled.
type captureLogHandler struct {
	onHandle func(slog.Record)
	preAttrs []slog.Attr
}

func (h *captureLogHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *captureLogHandler) Handle(_ context.Context, r slog.Record) error {
	full := r.Clone()
	for _, a := range h.preAttrs {
		full.AddAttrs(a)
	}
	h.onHandle(full)
	return nil
}
func (h *captureLogHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.preAttrs = append(append([]slog.Attr{}, h.preAttrs...), attrs...)
	return &c
}
func (h *captureLogHandler) WithGroup(_ string) slog.Handler { return h }
