package usenet

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

type syncStore struct {
	mu sync.Mutex
	m  map[string][]byte
}

func newSyncStore() *syncStore { return &syncStore{m: map[string][]byte{}} }
func (s *syncStore) Get(id string) ([]byte, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	return v, ok
}
func (s *syncStore) Put(id string, data []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = data
	return nil
}
func (s *syncStore) ids() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.m))
	for k := range s.m {
		out = append(out, k)
	}
	return out
}

func newKeepReader(t *testing.T, fp *fakepool.Client, store SegmentStore, n, segSize, prefetch int) *UsenetReader {
	t.Helper()
	ctx := context.Background()
	rg := buildEagerRange(ctx, t, n, segSize)
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, prefetch, noopMetrics{}, "keep-test", store, withFlightMap(newFlightMap()))
	if err != nil {
		t.Fatal(err)
	}
	return ur
}

func waitCalls(t *testing.T, fp *fakepool.Client, want int64) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for fp.BodyStreamPriorityCalls() < want && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := fp.BodyStreamPriorityCalls(); got < want {
		t.Fatalf("stream fetches = %d, want at least %d", got, want)
	}
}

func waitStored(t *testing.T, store *syncStore, id string, want []byte) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := store.Get(id); ok {
			if !bytes.Equal(got, want) {
				t.Fatalf("stored %d bytes for %s, want exact payload", len(got), id)
			}
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("article %s never reached the store", id)
}

// A reader closed while its demand-window article is mid-wire lets that
// article finish and stores it, so the next open of the same head is a hit
// rather than a second fetch (and the pool keeps its connection instead of
// aborting a body).
func TestClosedReaderFinishesStartedArticleIntoStore(t *testing.T) {
	const segSize = 4096
	fp := fakepool.New()
	gate := make(chan struct{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024, TailGate: gate,
	})
	store := newSyncStore()
	ur := newKeepReader(t, fp, store, 1, segSize, 4)
	ur.Start()
	waitCalls(t, fp, 1)
	time.Sleep(20 * time.Millisecond) // first chunk is written before the gate holds

	closed := make(chan struct{})
	go func() { _ = ur.Close(); close(closed) }()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close must not wait for the in-flight article")
	}
	if _, ok := store.Get(segments.MessageID(0)); ok {
		t.Fatal("article stored before its tail arrived")
	}

	close(gate)
	waitStored(t, store, segments.MessageID(0), segments.Payload(0, segSize))
	if got := fp.BodyStreamPriorityCalls(); got != 1 {
		t.Fatalf("stream fetches = %d, want 1 (no refetch after close)", got)
	}
}

// An article whose fetch has not delivered a byte yet is abandoned on close
// as before: there is nothing to finish and no reason to hold a connection.
func TestClosedReaderAbandonsArticleWithNoBytes(t *testing.T) {
	const segSize = 4096
	fp := fakepool.New()
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), Latency: 300 * time.Millisecond,
	})
	store := newSyncStore()
	ur := newKeepReader(t, fp, store, 1, segSize, 4)
	ur.Start()
	waitCalls(t, fp, 1)
	_ = ur.Close()

	time.Sleep(500 * time.Millisecond)
	if _, ok := store.Get(segments.MessageID(0)); ok {
		t.Fatal("an article with no bytes on the wire must not be completed after close")
	}
}

// Speculative prefetch beyond the demand window is abandoned on close even
// when bytes have started, so a seek does not keep the whole read-ahead
// window downloading.
func TestClosedReaderAbandonsSpeculativeArticles(t *testing.T) {
	const n, segSize = 6, 4096
	fp := fakepool.New()
	gate := make(chan struct{})
	for i := 0; i < n; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: segments.Payload(i, segSize), ChunkSize: 1024, TailGate: gate,
		})
	}
	store := newSyncStore()
	ur := newKeepReader(t, fp, store, n, segSize, n)
	ur.Start()
	waitCalls(t, fp, int64(n))
	time.Sleep(20 * time.Millisecond)
	_ = ur.Close()
	close(gate)

	for i := 0; i < demandDepth; i++ {
		waitStored(t, store, segments.MessageID(i), segments.Payload(i, segSize))
	}
	time.Sleep(100 * time.Millisecond)
	if got := len(store.ids()); got != demandDepth {
		t.Fatalf("stored %d articles after close, want only the %d demand-window ones: %v", got, demandDepth, store.ids())
	}
}

// A reader opened while a closed reader's demand-window article is still
// finishing joins that fetch as a follower instead of starting a second one,
// and reads the exact bytes from the shared buffer.
func TestLateReaderJoinsFinishingArticle(t *testing.T) {
	const segSize = 4096
	fp := fakepool.New()
	gate := make(chan struct{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024, TailGate: gate,
	})
	store := newSyncStore()
	fm := newFlightMap()
	mk := func() *UsenetReader {
		rg := buildEagerRange(context.Background(), t, 1, segSize)
		getter := func() (pool.NntpClient, error) { return fp, nil }
		ur, err := NewUsenetReader(context.Background(), getter, rg, 4, noopMetrics{}, "late-test", store, withFlightMap(fm))
		if err != nil {
			t.Fatal(err)
		}
		return ur
	}
	first := mk()
	first.Start()
	waitCalls(t, fp, 1)
	time.Sleep(20 * time.Millisecond)
	_ = first.Close()

	late := mk()
	late.Start()
	buf := make([]byte, segSize)
	done := make(chan error, 1)
	go func() {
		_, err := io.ReadFull(late, buf)
		done <- err
	}()
	time.Sleep(50 * time.Millisecond)
	close(gate)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("late reader: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late reader never completed")
	}
	if !bytes.Equal(buf, segments.Payload(0, segSize)) {
		t.Fatal("late reader got wrong bytes")
	}
	if got := fp.BodyStreamPriorityCalls(); got != 1 {
		t.Fatalf("stream fetches = %d, want 1 (late reader must join the finishing fetch)", got)
	}
	_ = late.Close()
}
