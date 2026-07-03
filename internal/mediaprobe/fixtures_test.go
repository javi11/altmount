package mediaprobe

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"testing"
)

// --- MP4 fixture builders ---

func mp4Box_(fourcc string, payload ...[]byte) []byte {
	var body bytes.Buffer
	for _, p := range payload {
		body.Write(p)
	}
	out := make([]byte, 8+body.Len())
	binary.BigEndian.PutUint32(out[:4], uint32(8+body.Len()))
	copy(out[4:8], fourcc)
	copy(out[8:], body.Bytes())
	return out
}

// mp4LargeBox builds a box using the 64-bit largesize encoding.
func mp4LargeBox(fourcc string, payload []byte) []byte {
	out := make([]byte, 16+len(payload))
	binary.BigEndian.PutUint32(out[:4], 1)
	copy(out[4:8], fourcc)
	binary.BigEndian.PutUint64(out[8:16], uint64(16+len(payload)))
	copy(out[16:], payload)
	return out
}

// mvhdV0 builds an mvhd payload (version 0) with the given timescale/duration.
func mvhdV0(timescale, duration uint32) []byte {
	p := make([]byte, 100)
	// version=0, flags=0, creation(4), modification(4) all zero
	binary.BigEndian.PutUint32(p[12:16], timescale)
	binary.BigEndian.PutUint32(p[16:20], duration)
	return p
}

// mvhdV1 builds an mvhd payload (version 1).
func mvhdV1(timescale uint32, duration uint64) []byte {
	p := make([]byte, 112)
	p[0] = 1
	binary.BigEndian.PutUint32(p[20:24], timescale)
	binary.BigEndian.PutUint64(p[24:32], duration)
	return p
}

// buildMP4 concatenates boxes into a file image.
func buildMP4(boxes ...[]byte) []byte {
	return bytes.Join(boxes, nil)
}

// --- MKV fixture builders ---

func ebmlID(id uint32) []byte {
	switch {
	case id <= 0xFF:
		return []byte{byte(id)}
	case id <= 0xFFFF:
		return []byte{byte(id >> 8), byte(id)}
	case id <= 0xFFFFFF:
		return []byte{byte(id >> 16), byte(id >> 8), byte(id)}
	default:
		return []byte{byte(id >> 24), byte(id >> 16), byte(id >> 8), byte(id)}
	}
}

// ebmlSize encodes a payload size as a 2-byte vint (max 0x3FFE).
func ebmlSize(n int) []byte {
	if n > 0x3FFE {
		panic("test fixture size too large for 2-byte vint")
	}
	return []byte{0x40 | byte(n>>8), byte(n)}
}

func mkvEl(id uint32, payload ...[]byte) []byte {
	var body bytes.Buffer
	for _, p := range payload {
		body.Write(p)
	}
	var out bytes.Buffer
	out.Write(ebmlID(id))
	out.Write(ebmlSize(body.Len()))
	out.Write(body.Bytes())
	return out.Bytes()
}

// mkvElUnknownSize emits an element with the 1-byte unknown-size marker.
func mkvElUnknownSize(id uint32, payload []byte) []byte {
	var out bytes.Buffer
	out.Write(ebmlID(id))
	out.WriteByte(0xFF)
	out.Write(payload)
	return out.Bytes()
}

func mkvUint(id uint32, v uint64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], v)
	i := 0
	for i < 7 && buf[i] == 0 {
		i++
	}
	return mkvEl(id, buf[i:])
}

func mkvFloat64(id uint32, v float64) []byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], math.Float64bits(v))
	return mkvEl(id, buf[:])
}

// buildMKV builds a minimal Matroska file: EBML header + Segment(children...).
func buildMKV(segmentChildren ...[]byte) []byte {
	header := mkvEl(ebmlHeaderID, make([]byte, 20))
	segment := mkvEl(mkvSegmentID, segmentChildren...)
	return append(header, segment...)
}

// mkvInfo builds an Info element with the given duration (seconds) using the
// default timestamp scale.
func mkvInfo(durationSec float64) []byte {
	return mkvEl(mkvInfoID,
		mkvUint(mkvTimestampScaleID, 1_000_000),
		mkvFloat64(mkvDurationID, durationSec*1000), // Duration is in timestamp-scale units (ms at default scale)
	)
}

// --- tracking reader ---

type readOp struct {
	off int64
	len int
}

// trackingReaderAt serves data and records every read. Reads intersecting
// missing ranges return ErrMissingRange, mimicking the missing-aware wrapper
// used in production.
type trackingReaderAt struct {
	data    []byte
	missing []ByteRange
	reads   []readOp
}

func (r *trackingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.reads = append(r.reads, readOp{off: off, len: len(p)})
	req := ByteRange{Start: off, End: off + int64(len(p)) - 1}
	for _, m := range r.missing {
		if rangesOverlap(req, m) {
			return 0, ErrMissingRange
		}
	}
	if off >= int64(len(r.data)) {
		return 0, io.EOF
	}
	n := copy(p, r.data[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func (r *trackingReaderAt) totalBytesRead() int {
	total := 0
	for _, op := range r.reads {
		total += op.len
	}
	return total
}

func (r *trackingReaderAt) assertNoReadIn(t *testing.T, forbidden ByteRange, label string) {
	t.Helper()
	for _, op := range r.reads {
		req := ByteRange{Start: op.off, End: op.off + int64(op.len) - 1}
		if rangesOverlap(req, forbidden) {
			t.Fatalf("probe read %d bytes at %d inside forbidden %s range [%d,%d]",
				op.len, op.off, label, forbidden.Start, forbidden.End)
		}
	}
}

// findRange returns the extent of the box/element with the given label in a
// probed structure, failing the test when absent.
func findRange(t *testing.T, ranges []ByteRange, label string) ByteRange {
	t.Helper()
	for _, r := range ranges {
		if r.Label == label {
			return r
		}
	}
	t.Fatalf("no range labeled %q in %v", label, ranges)
	return ByteRange{}
}

func fmtRanges(ranges []ByteRange) string {
	var b bytes.Buffer
	for _, r := range ranges {
		fmt.Fprintf(&b, "[%d,%d]%s ", r.Start, r.End, r.Label)
	}
	return b.String()
}
