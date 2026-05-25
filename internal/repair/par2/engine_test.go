package par2

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"
)

// mapFetcher serves present segment bytes straight from the original file. Any
// segment not in segs is reported as a permanently-missing hole, so the engine
// reconstructs it without ever seeing its bytes.
type mapFetcher struct {
	data []byte
	segs map[string]Segment
	hits int
}

func (m *mapFetcher) Fetch(_ context.Context, id string) ([]byte, bool, error) {
	m.hits++
	s, ok := m.segs[id]
	if !ok {
		return nil, true, nil // hole
	}
	return m.data[s.Start:s.End], false, nil
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

func TestMissingSlices(t *testing.T) {
	rs, data := loadFixture(t)
	ss := int(rs.SliceSize)
	segs := makeSegments(len(data), 5000)
	_, total := rs.Layout()

	// Nothing missing → no missing slices, total preserved.
	if mc, tot := MissingSlices(rs, segs, nil); mc != 0 || tot != total {
		t.Fatalf("no-missing: got (%d,%d), want (0,%d)", mc, tot, total)
	}

	// One missing segment affects exactly the slices spanning its byte range.
	missing := map[string]bool{segs[1].MessageID: true}
	mc, tot := MissingSlices(rs, segs, missing)
	if tot != total {
		t.Fatalf("total = %d, want %d", tot, total)
	}
	first := int(segs[1].Start) / ss
	last := int(segs[1].End-1) / ss
	if want := last - first + 1; mc != want {
		t.Fatalf("missing slices = %d, want %d", mc, want)
	}
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

	// The fetcher only knows about PRESENT segments; for the dropped ones it
	// returns a hole (no bytes). Reconstruction must still be byte-exact —
	// proving repair never depends on the data it is supposed to reconstruct.
	presentByID := make(map[string]Segment)
	for _, s := range segs {
		if !missing[s.MessageID] {
			presentByID[s.MessageID] = s
		}
	}
	fetch := &mapFetcher{data: data, segs: presentByID}
	sink := &mapSink{got: map[string][]byte{}}

	res, err := RepairFileSegments(context.Background(), rs, segs, fetch, sink)
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

// shortFetcher returns a correct body for every present segment except one,
// for which it returns a truncated body — simulating a present-but-corrupt
// article that is shorter than its declared byte range.
type shortFetcher struct {
	data    []byte
	segs    map[string]Segment
	shortID string
}

func (f *shortFetcher) Fetch(_ context.Context, id string) ([]byte, bool, error) {
	s, ok := f.segs[id]
	if !ok {
		return nil, true, nil // hole
	}
	full := f.data[s.Start:s.End]
	if id == f.shortID {
		return full[:len(full)/2], false, nil // present but truncated
	}
	return full, false, nil
}

// A present-but-truncated article must be treated as a hole and reconstructed —
// never scattered as-is, which would corrupt the slice or panic the scatter
// loop (it runs in a background goroutine with no recover()). Here segs[0] comes
// back short and segs[1] is absent; both are healed from PAR2.
func TestRepairFileSegmentsShortBody(t *testing.T) {
	rs, data := loadFixture(t)
	segs := makeSegments(len(data), 5000)

	presentByID := make(map[string]Segment)
	for _, s := range segs {
		presentByID[s.MessageID] = s
	}
	delete(presentByID, segs[1].MessageID) // segs[1] is a genuine hole
	fetch := &shortFetcher{data: data, segs: presentByID, shortID: segs[0].MessageID}
	sink := &mapSink{got: map[string][]byte{}}

	res, err := RepairFileSegments(context.Background(), rs, segs, fetch, sink)
	if err != nil {
		t.Fatalf("expected truncated+absent articles to heal, got error: %v", err)
	}

	segByID := make(map[string]Segment, len(segs))
	for _, s := range segs {
		segByID[s.MessageID] = s
	}
	for _, id := range []string{segs[0].MessageID, segs[1].MessageID} {
		s := segByID[id]
		if !bytes.Equal(sink.got[id], data[s.Start:s.End]) {
			t.Fatalf("segment %s reconstructed incorrectly", id)
		}
	}
	if len(res.Recovered) != 2 {
		t.Fatalf("recovered %d segments, want 2", len(res.Recovered))
	}
}

// A reconstructed slice that fails its IFSC checksum must surface as
// ErrVerificationFailed so the caller falls back instead of serving garbage.
func TestRepairFileSegmentsVerificationFailure(t *testing.T) {
	rs, data := loadFixture(t)
	segs := makeSegments(len(data), 5000)
	missing := map[string]bool{segs[1].MessageID: true}

	// Tamper with the IFSC checksums for the slices the missing segment spans, so
	// the (correctly) reconstructed bytes no longer match the recorded sums.
	fid := rs.RecoveryFileIDs[0]
	ss := int64(rs.SliceSize)
	first := segs[1].Start / ss
	last := (segs[1].End - 1) / ss
	for s := first; s <= last; s++ {
		rs.SliceCRCs[fid][s].CRC32 ^= 0xFFFFFFFF
	}

	presentByID := make(map[string]Segment)
	for _, s := range segs {
		if !missing[s.MessageID] {
			presentByID[s.MessageID] = s
		}
	}
	fetch := &mapFetcher{data: data, segs: presentByID}
	sink := &mapSink{got: map[string][]byte{}}

	_, err := RepairFileSegments(context.Background(), rs, segs, fetch, sink)
	if !errors.Is(err, ErrVerificationFailed) {
		t.Fatalf("got %v, want ErrVerificationFailed", err)
	}
}

func TestRepairFileSegmentsInsufficient(t *testing.T) {
	rs, data := loadFixture(t)
	// Tiny segments so that dropping many of them needs more than 8 slices.
	segs := makeSegments(len(data), 2048)
	// An empty present set makes the fetcher report every segment as a hole →
	// far more missing slices than the recovery set can cover.
	fetch := &mapFetcher{data: data, segs: map[string]Segment{}}
	sink := &mapSink{got: map[string][]byte{}}
	if _, err := RepairFileSegments(context.Background(), rs, segs, fetch, sink); err != ErrInsufficientRecovery {
		t.Fatalf("got %v, want ErrInsufficientRecovery", err)
	}
}
