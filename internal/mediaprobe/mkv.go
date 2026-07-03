package mediaprobe

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"math"
)

// Matroska/WebM element IDs (stored with the length-marker bit retained, as
// they appear on the wire).
const (
	ebmlHeaderID     = 0x1A45DFA3
	mkvSegmentID     = 0x18538067
	mkvSeekHeadID    = 0x114D9B74
	mkvInfoID        = 0x1549A966
	mkvTracksID      = 0x1654AE6B
	mkvCuesID        = 0x1C53BB6B
	mkvClusterID     = 0x1F43B675
	mkvChaptersID    = 0x1043A770
	mkvTagsID        = 0x1254C367
	mkvAttachmentsID = 0x1941A469
	mkvVoidID        = 0xEC
	mkvCRC32ID       = 0xBF
	// Info children
	mkvTimestampScaleID = 0x2AD7B1
	mkvDurationID       = 0x4489
)

const (
	// maxMKVElements bounds the segment-child walk; the walk stops early
	// anyway once all critical elements plus the first Cluster are seen.
	maxMKVElements = 4096
	unknownSize    = int64(-1)
)

type ebmlElement struct {
	id                 uint32
	start              int64 // element start (ID byte)
	dataStart, dataEnd int64 // payload extent, inclusive; dataEnd == fileEnd for unknown size
	size               int64 // payload size, unknownSize if not encoded
}

// probeMKV walks the top level of a Matroska/WebM file. EBML header, Info and
// Tracks are critical; Cues are seek-only; everything else (Clusters,
// SeekHead, Tags, Chapters, Attachments, Void) is payload. Once Info, Tracks
// and the first Cluster have been located, the remainder of the file is
// treated as Cluster space so the walk never iterates thousands of clusters
// (Cues at the tail end are degraded-safe either way).
func probeMKV(ctx context.Context, r io.ReaderAt, fileSize int64) (*Structure, error) {
	header, err := readEBMLElement(r, 0, fileSize)
	if err != nil {
		return nil, fmt.Errorf("mkv: %w", err)
	}
	if header.id != ebmlHeaderID {
		return nil, fmt.Errorf("mkv: file does not start with an EBML header (id 0x%X)", header.id)
	}
	if header.size == unknownSize {
		return nil, fmt.Errorf("mkv: EBML header has unknown size")
	}

	s := &Structure{Container: "mkv", FileSize: fileSize}
	s.Critical = append(s.Critical, ByteRange{Start: header.start, End: header.dataEnd, Label: "EBML header"})

	segment, err := readEBMLElement(r, header.dataEnd+1, fileSize)
	if err != nil {
		return nil, fmt.Errorf("mkv: %w", err)
	}
	if segment.id != mkvSegmentID {
		return nil, fmt.Errorf("mkv: expected Segment element, got id 0x%X", segment.id)
	}
	// The Segment element's own header bytes are needed to open the file.
	s.Critical = append(s.Critical, ByteRange{Start: segment.start, End: segment.dataStart - 1, Label: "Segment header"})

	sawInfo, sawTracks := false, false
	var infoElem *ebmlElement
	pos := segment.dataStart
	segEnd := segment.dataEnd
	for n := 0; pos <= segEnd; n++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if n >= maxMKVElements {
			return nil, fmt.Errorf("mkv: more than %d segment children", maxMKVElements)
		}
		el, err := readEBMLElement(r, pos, segEnd+1)
		if err != nil {
			return nil, fmt.Errorf("mkv: %w", err)
		}
		br := ByteRange{Start: el.start, End: el.dataEnd}
		switch el.id {
		case mkvInfoID:
			sawInfo = true
			br.Label = "Info"
			s.Critical = append(s.Critical, br)
			e := el
			infoElem = &e
		case mkvTracksID:
			sawTracks = true
			br.Label = "Tracks"
			s.Critical = append(s.Critical, br)
		case mkvCuesID:
			br.Label = "Cues"
			s.SeekOnly = append(s.SeekOnly, br)
		case mkvClusterID:
			if sawInfo && sawTracks {
				// Everything from the first Cluster to the end of the
				// segment is media/index space: degraded-safe.
				s.Payload = append(s.Payload, ByteRange{Start: el.start, End: segEnd, Label: "Clusters"})
				pos = segEnd + 1
				continue
			}
			if el.size == unknownSize {
				return nil, fmt.Errorf("mkv: unknown-size Cluster before Info/Tracks")
			}
			br.Label = "Cluster"
			s.Payload = append(s.Payload, br)
		default:
			br.Label = fmt.Sprintf("0x%X", el.id)
			s.Payload = append(s.Payload, br)
		}
		if el.size == unknownSize && el.id != mkvClusterID {
			return nil, fmt.Errorf("mkv: unknown-size element 0x%X", el.id)
		}
		pos = el.dataEnd + 1
	}
	if !sawInfo || !sawTracks {
		return nil, fmt.Errorf("mkv: missing critical elements (Info=%v Tracks=%v)", sawInfo, sawTracks)
	}

	if infoElem != nil {
		if dur, err := readMKVDuration(r, *infoElem); err == nil {
			s.DurationSeconds = dur
		}
	}
	return s, nil
}

// readMKVDuration parses Info's TimestampScale and Duration children.
func readMKVDuration(r io.ReaderAt, info ebmlElement) (float64, error) {
	timestampScale := float64(1_000_000) // Matroska default, in ns
	duration := float64(-1)
	pos := info.dataStart
	for n := 0; pos <= info.dataEnd && n < maxMKVElements; n++ {
		el, err := readEBMLElement(r, pos, info.dataEnd+1)
		if err != nil {
			return 0, err
		}
		if el.size == unknownSize {
			return 0, fmt.Errorf("mkv: unknown-size Info child 0x%X", el.id)
		}
		switch el.id {
		case mkvTimestampScaleID:
			v, err := readEBMLUint(r, el)
			if err != nil {
				return 0, err
			}
			if v > 0 {
				timestampScale = float64(v)
			}
		case mkvDurationID:
			v, err := readEBMLFloat(r, el)
			if err != nil {
				return 0, err
			}
			duration = v
		}
		pos = el.dataEnd + 1
	}
	if duration < 0 {
		return 0, fmt.Errorf("mkv: Duration element not found")
	}
	return duration * timestampScale / 1e9, nil
}

// readEBMLElement reads an element's ID and size vints starting at pos.
// limit is one past the last valid byte for this scope.
func readEBMLElement(r io.ReaderAt, pos, limit int64) (ebmlElement, error) {
	var buf [12]byte
	n := int64(len(buf))
	if pos+n > limit {
		n = limit - pos
	}
	if n < 2 {
		return ebmlElement{}, fmt.Errorf("element at %d: truncated", pos)
	}
	if _, err := r.ReadAt(buf[:n], pos); err != nil && err != io.EOF {
		return ebmlElement{}, fmt.Errorf("element at %d: %w", pos, err)
	}

	idLen := leadingVintLength(buf[0], 4)
	if idLen == 0 || int64(idLen) > n {
		return ebmlElement{}, fmt.Errorf("element at %d: invalid ID", pos)
	}
	var id uint32
	for i := 0; i < idLen; i++ {
		id = id<<8 | uint32(buf[i])
	}

	sizeLen := leadingVintLength(buf[idLen], 8)
	if sizeLen == 0 || int64(idLen+sizeLen) > n {
		return ebmlElement{}, fmt.Errorf("element 0x%X at %d: invalid size vint", id, pos)
	}
	size := int64(buf[idLen]) & (0xFF >> sizeLen)
	allOnes := size == int64(0xFF>>sizeLen)
	for i := 1; i < sizeLen; i++ {
		b := buf[idLen+i]
		size = size<<8 | int64(b)
		allOnes = allOnes && b == 0xFF
	}

	el := ebmlElement{
		id:        id,
		start:     pos,
		dataStart: pos + int64(idLen+sizeLen),
		size:      size,
	}
	if allOnes { // unknown size: extends to the end of the enclosing scope
		el.size = unknownSize
		el.dataEnd = limit - 1
	} else {
		el.dataEnd = el.dataStart + size - 1
		if el.dataEnd >= limit {
			return ebmlElement{}, fmt.Errorf("element 0x%X at %d: size %d exceeds scope", id, pos, size)
		}
	}
	return el, nil
}

// leadingVintLength returns the encoded length (1..maxLen) implied by the
// leading byte of an EBML vint, or 0 when invalid.
func leadingVintLength(b byte, maxLen int) int {
	if b == 0 {
		return 0
	}
	length := 1
	for mask := byte(0x80); mask > 0 && b&mask == 0; mask >>= 1 {
		length++
	}
	if length > maxLen {
		return 0
	}
	return length
}

func readEBMLUint(r io.ReaderAt, el ebmlElement) (uint64, error) {
	if el.size <= 0 || el.size > 8 {
		return 0, fmt.Errorf("mkv: invalid uint size %d", el.size)
	}
	buf := make([]byte, el.size)
	if _, err := r.ReadAt(buf, el.dataStart); err != nil {
		return 0, err
	}
	var v uint64
	for _, b := range buf {
		v = v<<8 | uint64(b)
	}
	return v, nil
}

func readEBMLFloat(r io.ReaderAt, el ebmlElement) (float64, error) {
	switch el.size {
	case 4:
		var buf [4]byte
		if _, err := r.ReadAt(buf[:], el.dataStart); err != nil {
			return 0, err
		}
		return float64(math.Float32frombits(binary.BigEndian.Uint32(buf[:]))), nil
	case 8:
		var buf [8]byte
		if _, err := r.ReadAt(buf[:], el.dataStart); err != nil {
			return 0, err
		}
		return math.Float64frombits(binary.BigEndian.Uint64(buf[:])), nil
	default:
		return 0, fmt.Errorf("mkv: invalid float size %d", el.size)
	}
}
