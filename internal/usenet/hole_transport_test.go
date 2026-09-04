package usenet

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"

	"github.com/javi11/altmount/internal/holes"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// Only a definite "no such article" may become a hole; a transport failure
// must fail the read without consulting the hole policy.
func TestTransportFailureNeverBecomesAHole(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 3, 64
	fp := fakepool.New()
	for i := 0; i < n; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize)})
	}
	fp.SetBehavior(segments.MessageID(1), fakepool.SegmentBehavior{Err: errors.New("connection reset by peer")})
	var onHole atomic.Int32
	hooks := &HoleHooks{OnHole: func(int, string) holes.Decision { onHole.Add(1); return holes.DecisionPad }}
	rg := buildEagerRange(ctx, t, n, segSize)
	ur := newReaderWithHooks(t, ctx, fp, rg, 4, hooks)
	_, err := io.ReadAll(ur)
	if err == nil {
		t.Fatal("a transport failure must fail the read")
	}
	if onHole.Load() != 0 {
		t.Fatal("OnHole must not be consulted for a transport failure")
	}
}
