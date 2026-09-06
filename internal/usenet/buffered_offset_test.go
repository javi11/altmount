package usenet

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// GetBufferedOffset reports scheduled bytes from counters and never
// re-materialises a released segment slot.
func TestBufferedOffsetDoesNotResurrectReleasedSegments(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 6, 128
	fp := fakepool.New()
	for i := 0; i < n; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: segments.Payload(i, segSize)})
	}
	loader := eagerLoader{n: n, size: segSize}
	rg := NewLazySegmentRange(ctx, 0, int64(n*segSize-1), loader, 0, 0, n-1, int64((n-1)*segSize))
	ur := newReaderForTest(t, ctx, fp, rg, 2)
	ur.Start()
	buf := make([]byte, 3*segSize)
	if _, err := io.ReadFull(ur, buf); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	off := ur.GetBufferedOffset()
	if off < 3*segSize || off > int64(n*segSize) {
		t.Fatalf("buffered offset %d out of range", off)
	}
	rg.mu.RLock()
	defer rg.mu.RUnlock()
	// Segments 0 and 1 are consumed and released; 2 is still current.
	for i := 0; i < 2; i++ {
		if rg.segments[i] != nil {
			t.Fatalf("consumed slot %d was re-materialised", i)
		}
	}
}

type eagerLoader struct{ n, size int }

func (l eagerLoader) GetSegment(i int) (Segment, []string, bool) {
	if i < 0 || i >= l.n {
		return Segment{}, nil, false
	}
	return Segment{Id: segments.MessageID(i), Start: 0, End: int64(l.size - 1), Size: int64(l.size)}, nil, true
}
