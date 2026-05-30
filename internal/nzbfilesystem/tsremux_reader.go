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
	inner      io.ReadCloser
	spans      []clipSpan
	absPos     int64        // absolute offset of the next byte to pull from inner
	packetSize int          // 192 (BDAV) or 188; 0 until detected
	disabled   bool         // true if the stream isn't recognisable TS → pure passthrough
	out        bytes.Buffer // rewritten bytes ready to deliver
	probe      []byte       // bytes read for packet-size detection, not yet framed
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

// fill pulls the next chunk from inner, rewrites it if it is a complete
// packet aligned within its clip, and appends it to out. Returns io.EOF when
// inner is exhausted.
func (r *tsRemuxReader) fill() error {
	// Detect packet size once from the head of the stream.
	if r.packetSize == 0 && !r.disabled {
		if err := r.detect(); err != nil {
			return err
		}
		if r.disabled {
			// detect() already moved any probed bytes into out as passthrough.
			return nil
		}
	}

	if r.disabled {
		return r.passthrough()
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
			rewritePacket(chunk, r.packetSize, clip.delta)
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

// detect reads up to two packets' worth from inner to determine the packet
// size, then frames from there. If the stream isn't recognisable TS, it sets
// disabled and emits whatever was probed as passthrough so no bytes are lost.
func (r *tsRemuxReader) detect() error {
	// Read enough to cover two BDAV packets for a confident detection.
	const probeLen = 2 * bdavPacketLen
	buf := make([]byte, probeLen)
	nr, err := io.ReadFull(r.inner, buf)
	buf = buf[:nr]
	r.probe = buf
	if nr == 0 {
		if err == io.ErrUnexpectedEOF {
			err = io.EOF
		}
		return err
	}

	ps := detectTSPacketSize(buf)
	if ps == 0 {
		// Not TS we understand — disable rewriting, stream raw.
		r.disabled = true
		r.out.Write(buf)
		r.absPos += int64(nr)
		r.probe = nil
		return nil
	}
	r.packetSize = ps

	// Frame the probed bytes packet-by-packet (they begin at r.absPos, which
	// is the reader's start — assumed packet-aligned for the head read; if it
	// isn't, the leading mid-packet bytes are emitted raw by the generic path).
	consumed := 0
	for consumed+ps <= len(buf) {
		clip := r.clipFor(r.absPos)
		pkt := buf[consumed : consumed+ps]
		intoClip := r.absPos - clipStartOrZero(clip)
		if clip != nil && intoClip%int64(ps) == 0 && r.absPos+int64(ps) <= clip.end+1 {
			rewritePacket(pkt, ps, clip.delta)
		}
		r.out.Write(pkt)
		r.absPos += int64(ps)
		consumed += ps
	}
	// Any trailing partial-packet bytes from the probe: stash so the next
	// fill() reads the rest of that packet and frames correctly. Simplest:
	// emit them raw (they are at most ps-1 bytes; a real stream's next read
	// continues the packet, but to keep framing simple at the probe seam we
	// pass these through). For BDAV with a packet-aligned start this branch
	// is never taken (probeLen is a multiple of 192).
	if consumed < len(buf) {
		r.out.Write(buf[consumed:])
		r.absPos += int64(len(buf) - consumed)
	}
	r.probe = nil
	if err == io.ErrUnexpectedEOF {
		err = nil // partial probe is fine; more may follow
	}
	return err
}

func clipStartOrZero(c *clipSpan) int64 {
	if c == nil {
		return 0
	}
	return c.start
}
