package nzbfilesystem

import (
	"bytes"
	"io"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// buildTwoClipMeta builds a synthetic byte-concatenation of two clips of BDAV
// packets that each carry both a PCR (adaptation field) and a PTS (PES header),
// and returns the raw bytes plus the matching ClipBoundary table. Both clips get
// a LARGE non-zero 90 kHz delta, so every packet's timestamp bytes change on
// rewrite — that is what makes a window that ends/starts mid-packet differ from
// the sequential reference when alignment is broken.
func buildTwoClipMeta(t *testing.T) (raw []byte, boundaries []*metapb.ClipBoundary) {
	t.Helper()
	const hz = 90000
	clip0Base := int64(11.65 * hz)
	clip1Base := int64(0.5 * hz)

	var buf bytes.Buffer
	mk := func(base int64, n int) {
		for i := range n {
			p := newBDAVPacket(0x100, true, 0x03) // adaptation + payload
			setPCR(p, base+int64(i)*hz)
			setPTS(p, base+int64(i)*hz)
			buf.Write(p)
		}
	}
	mk(clip0Base, 5) // clip 0: 5 packets
	clip0Len := int64(buf.Len())
	mk(clip1Base, 4) // clip 1: 4 packets
	total := int64(buf.Len())

	boundaries = []*metapb.ClipBoundary{
		{ByteLen: clip0Len, Delta_90K: int64(6445 * hz)},         // big lift
		{ByteLen: total - clip0Len, Delta_90K: int64(9000 * hz)}, // bigger lift
	}
	return buf.Bytes(), boundaries
}

// newRemuxMVF builds a minimal MetadataVirtualFile carrying a clip-boundary
// table so the continuous-timeline remux is active. No pool is needed: the
// raw-reader factory is injected by the test.
func newRemuxMVF(raw []byte, boundaries []*metapb.ClipBoundary) *MetadataVirtualFile {
	return &MetadataVirtualFile{
		meta: &fileHandleMeta{
			FileSize:       int64(len(raw)),
			ClipBoundaries: boundaries,
		},
	}
}

func TestAlignStartDown(t *testing.T) {
	// Two clips: [0,959] (5 pkts) and [960,1727] (4 pkts) — 192-byte grid per clip.
	spans := buildClipSpans([]*metapb.ClipBoundary{
		{ByteLen: 960, Delta_90K: 1},
		{ByteLen: 768, Delta_90K: 2},
	})
	cases := []struct{ off, want int64 }{
		{0, 0},       // clip0 start
		{1, 0},       // mid first packet → back to 0
		{191, 0},     // last byte of packet 0
		{192, 192},   // packet 1 start
		{193, 192},   // mid packet 1
		{960, 960},   // clip1 start (new grid origin)
		{961, 960},   // mid clip1 packet 0
		{1151, 960},  // last byte of clip1 packet 0
		{1152, 1152}, // clip1 packet 1 start
		{2000, 2000}, // past last span → unchanged
	}
	for _, c := range cases {
		if got := alignStartDown(spans, c.off); got != c.want {
			t.Errorf("alignStartDown(%d) = %d, want %d", c.off, got, c.want)
		}
	}
}

func TestAlignEndUp(t *testing.T) {
	spans := buildClipSpans([]*metapb.ClipBoundary{
		{ByteLen: 960, Delta_90K: 1},
		{ByteLen: 768, Delta_90K: 2},
	})
	fileSize := int64(1728)
	cases := []struct{ end, want int64 }{
		{0, 191},     // mid packet 0 → up to packet 0 end
		{191, 191},   // already at packet 0 end
		{192, 383},   // mid packet 1 → packet 1 end
		{959, 959},   // clip0 last byte (already aligned, clamped to span end)
		{960, 1151},  // clip1 packet 0 → its end
		{1727, 1727}, // file last byte, clamped to FileSize-1
		{2000, 2000}, // past last span → unchanged
	}
	for _, c := range cases {
		if got := alignEndUp(spans, c.end, fileSize); got != c.want {
			t.Errorf("alignEndUp(%d) = %d, want %d", c.end, got, c.want)
		}
	}
}

func TestSkipLimitReader(t *testing.T) {
	src := []byte("0123456789ABCDEF")
	// skip 3, limit 5 → "34567"
	r := newSkipLimitReader(newMem(src), 3, 5)
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if string(got) != "34567" {
		t.Errorf("got %q, want %q", got, "34567")
	}

	// chunk-size invariance: same result regardless of caller buffer size.
	for _, chunk := range []int{1, 2, 4, 16} {
		r := newSkipLimitReader(newMem(src), 3, 5)
		var out bytes.Buffer
		p := make([]byte, chunk)
		for {
			n, err := r.Read(p)
			out.Write(p[:n])
			if err == io.EOF {
				break
			}
			if err != nil {
				t.Fatalf("chunk %d: %v", chunk, err)
			}
		}
		if out.String() != "34567" {
			t.Errorf("chunk %d: got %q, want %q", chunk, out.String(), "34567")
		}
	}
}

// TestWrapAlignedRemux_WindowIndependence is the regression guard for the Plex
// stutter bug: the bytes returned for any window [start,end] must equal the
// corresponding slice of the full sequential rewrite, regardless of whether the
// window starts or ends mid-packet. This is exactly the ephemeral ReadAtContext
// path Plex/ffprobe hammer with unaligned ranged reads.
func TestWrapAlignedRemux_WindowIndependence(t *testing.T) {
	raw, boundaries := buildTwoClipMeta(t)
	mvf := newRemuxMVF(raw, boundaries)

	// Reference: the full sequential rewrite (what VLC's linear read sees).
	if !mvf.remuxActive() {
		t.Fatal("remux should be active for a clip-boundary file")
	}
	ref, err := io.ReadAll(newTSRemuxReader(newMem(raw), mvf.clipSpans, 0))
	if err != nil {
		t.Fatalf("reference ReadAll: %v", err)
	}
	if len(ref) != len(raw) {
		t.Fatalf("rewrite changed length: %d != %d", len(ref), len(raw))
	}

	rawOpen := func(s, e int64) (io.ReadCloser, error) {
		return newMem(raw[s : e+1]), nil
	}

	total := int64(len(raw))
	// Sweep starts at fine granularity (catches mid-packet, mid-PTS-field,
	// mid-PCR-field) and ends at a mix of lengths including ones that end
	// mid-packet and ones that cross the clip boundary.
	for start := int64(0); start < total; start += 7 {
		for _, length := range []int64{1, 5, 96, 191, 192, 193, 384, 500, total} {
			end := start + length - 1
			if end >= total {
				end = total - 1
			}
			if end < start {
				continue
			}
			r, err := mvf.wrapAlignedRemux(start, end, rawOpen)
			if err != nil {
				t.Fatalf("wrapAlignedRemux(%d,%d): %v", start, end, err)
			}
			got, err := io.ReadAll(r)
			_ = r.Close()
			if err != nil {
				t.Fatalf("read window [%d,%d]: %v", start, end, err)
			}
			want := ref[start : end+1]
			if !bytes.Equal(got, want) {
				t.Fatalf("window [%d,%d] (len %d) differs from sequential reference\n got %x\nwant %x",
					start, end, end-start+1, firstDiff(got, want), firstDiff(want, got))
			}
		}
	}
}

// firstDiff returns a short slice around the first differing byte for readable
// failure output.
func firstDiff(a, b []byte) []byte {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := range n {
		if a[i] != b[i] {
			lo := i - 2
			if lo < 0 {
				lo = 0
			}
			hi := i + 6
			if hi > len(a) {
				hi = len(a)
			}
			return a[lo:hi]
		}
	}
	return nil
}
