package par2

import (
	"bytes"
	"context"
	"fmt"
	"testing"
)

// mapFetcher serves present segment bytes straight from the original file.
type mapFetcher struct {
	data []byte
	segs map[string]Segment
	hits int
}

func (m *mapFetcher) Fetch(_ context.Context, id string) ([]byte, error) {
	m.hits++
	s, ok := m.segs[id]
	if !ok {
		return nil, fmt.Errorf("unknown segment %s", id)
	}
	return m.data[s.Start:s.End], nil
}

// mapSink collects reconstructed segments.
type mapSink struct{ got map[string][]byte }

func (m *mapSink) Put(id string, data []byte) error {
	m.got[id] = append([]byte(nil), data...)
	return nil
}

// makeSegments tiles a file of length n into contiguous segments of segSize
// (last one short), with deterministic message-IDs.
func makeSegments(n, segSize int) []Segment {
	var segs []Segment
	for off := 0; off < n; off += segSize {
		end := off + segSize
		if end > n {
			end = n
		}
		segs = append(segs, Segment{
			MessageID: fmt.Sprintf("<seg%d@altmount>", off/segSize),
			Start:     int64(off),
			End:       int64(end),
		})
	}
	return segs
}

func TestRepairFileSegments(t *testing.T) {
	rs, data := loadFixture(t)

	// 5000-byte segments over a 4096-byte slice grid → segments straddle slice
	// boundaries, exercising the segment↔slice mapping.
	segs := makeSegments(len(data), 5000)
	segByID := make(map[string]Segment, len(segs))
	for _, s := range segs {
		segByID[s.MessageID] = s
	}

	// Drop two non-adjacent segments (each ~1.2 slices → a few missing slices,
	// well within the 8 recovery blocks).
	missingIDs := []string{segs[1].MessageID, segs[5].MessageID}
	missing := map[string]bool{}
	for _, id := range missingIDs {
		missing[id] = true
	}

	// The fetcher only knows about PRESENT segments: if the engine ever tries
	// to fetch a missing one, Fetch errors and the test fails — proving repair
	// never depends on the data it is supposed to reconstruct.
	presentByID := make(map[string]Segment)
	for _, s := range segs {
		if !missing[s.MessageID] {
			presentByID[s.MessageID] = s
		}
	}
	fetch := &mapFetcher{data: data, segs: presentByID}
	sink := &mapSink{got: map[string][]byte{}}

	res, err := RepairFileSegments(context.Background(), rs, segs, missing, fetch, sink)
	if err != nil {
		t.Fatalf("RepairFileSegments: %v", err)
	}
	if len(res.Recovered) != len(missingIDs) {
		t.Fatalf("recovered %d segments, want %d", len(res.Recovered), len(missingIDs))
	}

	// Reconstructed segment bytes must match the original file ranges exactly.
	for _, id := range missingIDs {
		s := segByID[id]
		want := data[s.Start:s.End]
		got, ok := sink.got[id]
		if !ok {
			t.Fatalf("segment %s not delivered to sink", id)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("segment %s reconstructed incorrectly (len got=%d want=%d)", id, len(got), len(want))
		}
	}
}

func TestRepairFileSegmentsInsufficient(t *testing.T) {
	rs, data := loadFixture(t)
	// Tiny segments so that dropping many of them needs more than 8 slices.
	segs := makeSegments(len(data), 2048)
	missing := map[string]bool{}
	for i := 0; i < len(segs); i++ { // drop everything → far exceeds recovery
		missing[segs[i].MessageID] = true
	}
	fetch := &mapFetcher{data: data, segs: map[string]Segment{}}
	sink := &mapSink{got: map[string][]byte{}}
	if _, err := RepairFileSegments(context.Background(), rs, segs, missing, fetch, sink); err != ErrInsufficientRecovery {
		t.Fatalf("got %v, want ErrInsufficientRecovery", err)
	}
}
