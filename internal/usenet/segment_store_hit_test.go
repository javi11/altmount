package usenet

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

type mapStore struct{ m map[string][]byte }

func (s mapStore) Get(id string) ([]byte, bool) { v, ok := s.m[id]; return v, ok }
func (s mapStore) Put(id string, data []byte) error {
	s.m[id] = data
	return nil
}

// A segment served from the store must reach the reader without a fetch, for
// streaming and import readers alike.
func TestSegmentStoreHitIsServedWithoutFetch(t *testing.T) {
	for _, imp := range []bool{false, true} {
		ctx := context.Background()
		const n, segSize = 3, 256
		fp := fakepool.New()
		store := mapStore{m: map[string][]byte{}}
		for i := 0; i < n; i++ {
			store.m[segments.MessageID(i)] = segments.Payload(i, segSize)
		}
		rg := buildEagerRange(ctx, t, n, segSize)
		getter := func() (pool.NntpClient, error) { return fp, nil }
		opts := []ReaderOption{withFlightMap(newFlightMap())}
		if imp {
			opts = append(opts, WithImportProfile(nil))
		}
		ur, err := NewUsenetReader(ctx, getter, rg, 4, noopMetrics{}, "store-test", store, opts...)
		if err != nil {
			t.Fatal(err)
		}
		ur.Start()
		done := make(chan []byte, 1)
		go func() { got, _ := io.ReadAll(ur); done <- got }()
		select {
		case got := <-done:
			if !bytes.Equal(got, segments.FileBytes(n, segSize)) {
				t.Fatalf("import=%v: bytes differ", imp)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("import=%v: read hung on a store hit", imp)
		}
		if fp.BodyPriorityCalls()+fp.BodyCalls() != 0 {
			t.Fatalf("import=%v: store hits must not fetch", imp)
		}
		_ = ur.Close()
	}
}
