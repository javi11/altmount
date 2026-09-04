package nzbfilesystem

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// A player's first read must complete while the rest of the first article is
// still arriving.
func TestFirstByteArrivesBeforeArticleCompletes(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 4, 64 << 10
	fp := fakepool.New()
	gate := make(chan struct{})
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{ChunkSize: 4096})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 4096, TailGate: gate,
	})
	mvf := newTestMVF(t, ctx, fp, n, segSize, 2)

	buf := make([]byte, 1024)
	done := make(chan error, 1)
	go func() { _, err := mvf.ReadAt(buf, 0); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadAt did not return while the article tail was held")
	}
	close(gate)
	rest := make([]byte, n*segSize-1024)
	if _, err := mvf.ReadAt(rest, 1024); err != nil {
		t.Fatal(err)
	}
	want := segments.FileBytes(n, segSize)
	if string(buf) != string(want[:1024]) || string(rest) != string(want[1024:]) {
		t.Fatal("streamed bytes differ from payload")
	}
}
