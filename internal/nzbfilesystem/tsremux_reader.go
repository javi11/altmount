package nzbfilesystem

import (
	"bytes"
	"io"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// clipSpan is one clip's absolute byte range in the virtual file plus the
// 90 kHz timeline delta to add to every timestamp inside it.
type clipSpan struct {
	start int64 // inclusive absolute byte offset
	end   int64 // inclusive absolute byte offset (start + byteLen - 1)
	delta int64 // 90 kHz offset added to PTS/DTS/PCR-base of packets in this clip
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

// spanContaining returns the span whose [start,end] range covers off, or nil
// when off is past the last clip (a passthrough region with no grid). It mirrors
// tsRemuxReader.clipFor but is a free function usable before a reader exists.
func spanContaining(spans []clipSpan, off int64) *clipSpan {
	lo, hi := 0, len(spans)-1
	idx := -1
	for lo <= hi {
		mid := (lo + hi) / 2
		if spans[mid].start <= off {
			idx = mid
			lo = mid + 1
		} else {
			hi = mid - 1
		}
	}
	if idx < 0 || off > spans[idx].end {
		return nil
	}
	return &spans[idx]
}

// alignStartDown rounds off DOWN to the start of the BDAV packet that contains
// it, using the containing clip's byte start as the grid origin (each clip's
// bytes begin a fresh 192-byte packet grid). Offsets past the last clip are
// returned unchanged — that region, if any, is pure passthrough.
func alignStartDown(spans []clipSpan, off int64) int64 {
	sp := spanContaining(spans, off)
	if sp == nil {
		return off
	}
	return off - ((off - sp.start) % bdavPacketLen)
}

// alignEndUp rounds end UP to the last byte of the BDAV packet that contains it
// (grid origin = containing clip start), clamped to that clip's end and to
// fileSize-1. Offsets past the last clip are returned unchanged.
func alignEndUp(spans []clipSpan, end, fileSize int64) int64 {
	sp := spanContaining(spans, end)
	if sp == nil {
		return end
	}
	into := end - sp.start
	aligned := sp.start + ((into/bdavPacketLen)+1)*bdavPacketLen - 1
	if aligned > sp.end {
		aligned = sp.end
	}
	if fileSize > 0 && aligned > fileSize-1 {
		aligned = fileSize - 1
	}
	return aligned
}

// skipLimitReader presents exactly [skip, skip+limit) of its inner reader: it
// discards the first `skip` bytes (lazily, on first Read) and then delivers at
// most `limit` bytes. It is the trim half of packet-grid alignment — the inner
// reader is opened over a packet-aligned window so the remux always sees whole
// packets, and this wrapper clips the rewritten bytes back to the exact window
// the caller requested, keeping file offsets and sizes unchanged.
type skipLimitReader struct {
	inner     io.ReadCloser
	skip      int64
	limit     int64
	delivered int64
	skipped   bool
}

func newSkipLimitReader(inner io.ReadCloser, skip, limit int64) *skipLimitReader {
	if skip < 0 {
		skip = 0
	}
	if limit < 0 {
		limit = 0
	}
	return &skipLimitReader{inner: inner, skip: skip, limit: limit}
}

func (s *skipLimitReader) Read(p []byte) (int, error) {
	if !s.skipped {
		if s.skip > 0 {
			if _, err := io.CopyN(io.Discard, s.inner, s.skip); err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return 0, err
			}
		}
		s.skipped = true
	}
	remaining := s.limit - s.delivered
	if remaining <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := s.inner.Read(p)
	s.delivered += int64(n)
	return n, err
}

func (s *skipLimitReader) Close() error { return s.inner.Close() }

// tsRemuxReader wraps an underlying reader that yields the bytes of a
// byte-concatenated multi-clip Blu-ray main feature starting at absolute offset
// startOff. As bytes stream through, it frames them into BDAV/TS source packets
// (aligned to each clip's byte start) and adds that clip's 90 kHz delta to the
// PTS/DTS/PCR timestamps, producing a single continuous timeline. The transform
// is byte-length preserving, so the wrapper is a drop-in io.ReadCloser that does
// not change offsets or sizes.
//
// It is a streaming reader: it buffers across Read calls so packet framing is
// maintained for an entire sequential run. A read that starts mid-packet emits
// its leading partial bytes unrewritten, and a read whose underlying window ends
// mid-packet emits the trailing partial packet unrewritten. Callers that need a
// byte-window's output to match the full sequential rewrite therefore open the
// underlying reader over a packet-ALIGNED window (alignStartDown/alignEndUp) and
// trim it back with skipLimitReader; see MetadataVirtualFile.wrapAlignedRemux.
type tsRemuxReader struct {
	inner       io.ReadCloser
	spans       []clipSpan
	absPos      int64        // absolute offset of the next byte to pull from inner
	packetSize  int          // 192 (BDAV); fixed for BD main features
	disabled    bool         // true if the stream isn't recognisable TS → pure passthrough
	syncChecked bool         // whether the first aligned packet's sync byte was validated
	out         bytes.Buffer // rewritten bytes ready to deliver
	scratch     []byte       // reusable read buffer for fill()/passthrough() (avoids per-call alloc)
}

// readBuf returns a reusable scratch slice of length n. n is always small
// (≤ packetSize for fill, 64 KiB for passthrough), so a single lazily-allocated
// 64 KiB buffer backs every read and no allocation happens on the streaming path.
func (r *tsRemuxReader) readBuf(n int) []byte {
	if cap(r.scratch) < n {
		size := 64 * 1024
		if n > size {
			size = n
		}
		r.scratch = make([]byte, size)
	}
	return r.scratch[:n]
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

	chunk := r.readBuf(want)
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
	chunk := r.readBuf(64 * 1024)
	nr, err := r.inner.Read(chunk)
	if nr > 0 {
		r.out.Write(chunk[:nr])
		r.absPos += int64(nr)
	}
	return err
}
