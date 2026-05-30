package nzbfilesystem

import (
	"bytes"
	"io"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// memReadCloser serves a byte slice as an io.ReadCloser.
type memReadCloser struct{ r *bytes.Reader }

func newMem(b []byte) *memReadCloser { return &memReadCloser{r: bytes.NewReader(b)} }
func (m *memReadCloser) Read(p []byte) (int, error) { return m.r.Read(p) }
func (m *memReadCloser) Close() error               { return nil }

// buildTwoClipStream builds a raw byte concatenation of two clips of BDAV
// packets (each packet carrying a PTS), plus the clipSpans that lift them onto
// one continuous timeline keeping clip 0's native base. Returns the raw bytes,
// the spans, and the expected monotonic PTS sequence after rewrite.
func buildTwoClipStream(t *testing.T) (raw []byte, spans []clipSpan, wantPTS []int64) {
	t.Helper()
	const hz = 90000
	clip0Base := int64(11.65 * hz)
	clip1Base := int64(0.5 * hz)
	clip0Dur := int64(30 * hz)

	var buf bytes.Buffer
	mk := func(base int64, n int) {
		for i := range n {
			buf.Write(setPTS(newBDAVPacket(0x100, true, 0x01), base+int64(i)*hz))
		}
	}
	mk(clip0Base, 4) // clip 0: 4 packets
	clip0Len := int64(buf.Len())
	mk(clip1Base, 3) // clip 1: 3 packets
	total := int64(buf.Len())

	base0 := clip0Base
	timelineStart1 := base0 + clip0Dur
	spans = []clipSpan{
		{start: 0, end: clip0Len - 1, delta: base0 - clip0Base},               // 0
		{start: clip0Len, end: total - 1, delta: timelineStart1 - clip1Base},  // lift clip1
	}

	// Expected PTS after rewrite.
	for i := range 4 {
		wantPTS = append(wantPTS, clip0Base+int64(i)*hz) // delta 0
	}
	for i := range 3 {
		wantPTS = append(wantPTS, timelineStart1+int64(i)*hz)
	}
	return buf.Bytes(), spans, wantPTS
}

// ptsAtPacket decodes the PTS from the n-th 192-byte BDAV packet in b.
func ptsAtPacket(b []byte, n int) int64 {
	pkt := b[n*bdavPacketLen : (n+1)*bdavPacketLen]
	// PES payload at TS offset 4 → BDAV offset 8; PTS at payload+9.
	return readTS(pkt[8:][9:14])
}

func TestTSRemuxReader_FullReadMonotonic(t *testing.T) {
	raw, spans, wantPTS := buildTwoClipStream(t)

	out, err := io.ReadAll(newTSRemuxReader(newMem(raw), spans, 0))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(out) != len(raw) {
		t.Fatalf("output length %d != input length %d (must be byte-preserving)", len(out), len(raw))
	}
	npkt := len(out) / bdavPacketLen
	var prev int64 = -1
	for i := range npkt {
		got := ptsAtPacket(out, i)
		if got != wantPTS[i] {
			t.Errorf("packet %d PTS = %d, want %d", i, got, wantPTS[i])
		}
		if got <= prev {
			t.Errorf("PTS not monotonic at packet %d: %d <= %d", i, got, prev)
		}
		prev = got
	}
}

// TestTSRemuxReader_ChunkSizeInvariant: the rewritten output must be identical
// regardless of the Read buffer size the caller uses (streaming determinism).
func TestTSRemuxReader_ChunkSizeInvariant(t *testing.T) {
	raw, spans, _ := buildTwoClipStream(t)
	full, _ := io.ReadAll(newTSRemuxReader(newMem(raw), spans, 0))

	for _, chunk := range []int{1, 7, 100, 192, 193, 1000} {
		r := newTSRemuxReader(newMem(raw), spans, 0)
		var got bytes.Buffer
		p := make([]byte, chunk)
		for {
			n, err := r.Read(p)
			got.Write(p[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("chunk %d: read error %v", chunk, err)
			}
		}
		if !bytes.Equal(got.Bytes(), full) {
			t.Errorf("chunk size %d produced different bytes than full read", chunk)
		}
	}
}

// TestTSRemuxReader_RangeDeterminism is the critical property for HTTP range
// requests: a wrapper started at an arbitrary packet-aligned mid-stream offset
// must produce exactly the same bytes as the corresponding slice of the full
// rewrite. This guarantees seeks/range GETs see a consistent timeline.
func TestTSRemuxReader_RangeDeterminism(t *testing.T) {
	raw, spans, _ := buildTwoClipStream(t)
	full, _ := io.ReadAll(newTSRemuxReader(newMem(raw), spans, 0))

	for startPkt := 0; startPkt*bdavPacketLen < len(raw); startPkt++ {
		startOff := int64(startPkt * bdavPacketLen)
		// Underlying reader yields bytes from startOff onward.
		r := newTSRemuxReader(newMem(raw[startOff:]), spans, startOff)
		got, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("startOff %d: %v", startOff, err)
		}
		want := full[startOff:]
		if !bytes.Equal(got, want) {
			t.Errorf("startOff %d: range read differs from full-rewrite slice", startOff)
		}
	}
}

// TestTSRemuxReader_NonTSPassthrough: a stream that isn't recognisable TS is
// passed through byte-for-byte (disabled mode), never corrupted.
func TestTSRemuxReader_NonTSPassthrough(t *testing.T) {
	raw := bytes.Repeat([]byte{0x11, 0x22, 0x33, 0x44}, 500) // no 0x47 sync grid
	spans := []clipSpan{{start: 0, end: int64(len(raw)) - 1, delta: 90000}}
	out, err := io.ReadAll(newTSRemuxReader(newMem(raw), spans, 0))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !bytes.Equal(out, raw) {
		t.Error("non-TS stream was modified; expected byte-for-byte passthrough")
	}
}

func TestBuildClipSpans(t *testing.T) {
	// Empty → nil (remux disabled).
	if buildClipSpans(nil) != nil {
		t.Error("buildClipSpans(nil) should be nil")
	}
	// Prefix sums turn (byte_len, delta) into absolute [start,end] ranges.
	spans := buildClipSpans([]*metapb.ClipBoundary{
		{ByteLen: 100, Delta_90K: 0},
		{ByteLen: 50, Delta_90K: 91000},
		{ByteLen: 200, Delta_90K: 272000},
	})
	want := []clipSpan{
		{start: 0, end: 99, delta: 0},
		{start: 100, end: 149, delta: 91000},
		{start: 150, end: 349, delta: 272000},
	}
	if len(spans) != len(want) {
		t.Fatalf("got %d spans, want %d", len(spans), len(want))
	}
	for i := range want {
		if spans[i] != want[i] {
			t.Errorf("span %d = %+v, want %+v", i, spans[i], want[i])
		}
	}
}
