package nzbfilesystem

import (
	"bytes"
	"io"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// clipSpan is one clip's absolute byte range in the virtual file plus the
// 90 kHz timeline delta to add to every timestamp inside it.
type clipSpan struct {
	start  int64 // inclusive absolute byte offset
	end    int64 // inclusive absolute byte offset (start + byteLen - 1)
	delta  int64 // 90 kHz offset added to PTS/DTS/PCR-base of packets in this clip
}

// buildClipSpans turns the proto ClipBoundary table (byte_len + delta per clip,
// in output order) into absolute byte ranges via a prefix sum. Returns nil
// when the table is empty, which keeps the remux disabled.
func buildClipSpans(boundaries []*metapb.ClipBoundary) []clipSpan {
	if len(boundaries) == 0 {
		return nil
	}
	spans := make([]clipSpan, 0, len(boundaries))
	var off int64
	for _, b := range boundaries {
		if b.ByteLen <= 0 {
			continue
		}
		spans = append(spans, clipSpan{start: off, end: off + b.ByteLen - 1, delta: b.Delta_90K})
		off += b.ByteLen
	}
	if len(spans) == 0 {
		return nil
	}
	return spans
}

// tsRemuxReader wraps an underlying reader that yields the bytes of a
// byte-concatenated multi-clip Blu-ray main feature starting at absolute offset
// startOff. As bytes stream through, it frames them into BDAV/TS source packets
// (aligned to each clip's byte start) and adds that clip's 90 kHz delta to the
// PTS/DTS/PCR timestamps, producing a single continuous timeline. The transform
// is byte-length preserving, so the wrapper is a drop-in io.ReadCloser that does
// not change offsets or sizes.
//
// It is a streaming reader: it buffers across Read calls so packet framing is
// maintained for an entire sequential run. Only the leading bytes of a read
// that starts mid-packet are passed through unrewritten (their timestamps, if
// any, live in the packet header before startOff); every fully-streamed packet
// is rewritten.
type tsRemuxReader struct {
	inner       io.ReadCloser
	spans       []clipSpan
	absPos      int64        // absolute offset of the next byte to pull from inner
	packetSize  int          // 192 (BDAV); fixed for BD main features
	disabled    bool         // true if the stream isn't recognisable TS → pure passthrough
	syncChecked bool         // whether the first aligned packet's sync byte was validated
	out         bytes.Buffer // rewritten bytes ready to deliver
}

// newTSRemuxReader wraps inner. startOff is the absolute file offset of inner's
// first byte. spans must be non-empty (callers gate on that).
func newTSRemuxReader(inner io.ReadCloser, spans []clipSpan, startOff int64) *tsRemuxReader {
	return &tsRemuxReader{inner: inner, spans: spans, absPos: startOff}
}

func (r *tsRemuxReader) Close() error { return r.inner.Close() }

// clipFor returns the span containing absolute offset off, or nil if past the
// last clip (then bytes are passed through raw).
func (r *tsRemuxReader) clipFor(off int64) *clipSpan {
	// Binary search: find the last span whose start <= off.
	lo, hi := 0, len(r.spans)-1
	idx := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		if r.spans[mid].start <= off {
			idx = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if idx < 0 || off > r.spans[idx].end {
		return nil
	}
	return &r.spans[idx]
}

func (r *tsRemuxReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	// Fill `out` until it can satisfy the request or inner is exhausted.
	for r.out.Len() < len(p) {
		if err := r.fill(); err != nil {
			if r.out.Len() > 0 {
				break // deliver what we have; surface the error on the next call
			}
			n, _ := r.out.Read(p)
			return n, err
		}
	}
	return r.out.Read(p)
}

// fill pulls the next chunk from inner, rewrites it if it is a complete packet
// aligned within its clip, and appends it to out. Returns io.EOF when inner is
// exhausted.
//
// Packet framing is derived from the CLIP grid (each clip's bytes start at
// clip.start and are a whole number of 192-byte BDAV source packets), NOT from
// probing the stream head. This is what makes the wrapper correct for reads
// that begin at an arbitrary (unaligned) offset — e.g. ffprobe seeking to
// near-EOF to estimate duration. A start that lands mid-packet emits the
// leading partial bytes raw, then frames full packets from the next boundary.
func (r *tsRemuxReader) fill() error {
	if r.disabled {
		return r.passthrough()
	}
	if r.packetSize == 0 {
		// BD main features (the only files with a clip table) are BDAV-192.
		r.packetSize = bdavPacketLen
	}

	clip := r.clipFor(r.absPos)
	if clip == nil {
		// Past the last clip (shouldn't happen for a well-formed table) —
		// stream the remainder unmodified.
		return r.passthrough()
	}

	// Bytes remaining to the next packet boundary within this clip.
	intoClip := r.absPos - clip.start
	rem := r.packetSize - int(intoClip%int64(r.packetSize))
	aligned := rem == r.packetSize
	want := rem
	// Never read across a clip boundary in one chunk.
	if r.absPos+int64(want) > clip.end+1 {
		want = int(clip.end + 1 - r.absPos)
		aligned = false // a clip whose length isn't a packet multiple: tail passthrough
	}

	chunk := make([]byte, want)
	nr, err := io.ReadFull(r.inner, chunk)
	chunk = chunk[:nr]
	if nr > 0 {
		if aligned && nr == r.packetSize {
			// Validate the first aligned packet looks like BDAV TS; if not,
			// the stream isn't what we expect (wrong decryption, plain TS,
			// non-media) so disable rewriting rather than corrupt bytes.
			if !r.syncChecked {
				r.syncChecked = true
				if chunk[4] != tsSync {
					r.disabled = true
				}
			}
			if !r.disabled {
				rewritePacket(chunk, r.packetSize, clip.delta)
			}
		}
		r.out.Write(chunk)
		r.absPos += int64(nr)
	}
	if err == io.ErrUnexpectedEOF {
		err = io.EOF
	}
	return err
}

// passthrough copies a chunk from inner to out without rewriting.
func (r *tsRemuxReader) passthrough() error {
	chunk := make([]byte, 64*1024)
	nr, err := r.inner.Read(chunk)
	if nr > 0 {
		r.out.Write(chunk[:nr])
		r.absPos += int64(nr)
	}
	return err
}
