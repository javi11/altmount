package mediaprobe

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
)

const (
	// maxTopLevelBoxes bounds the walk; real files have well under 100
	// top-level boxes (fragmented MP4s with thousands of moof boxes are
	// aborted rather than walked exhaustively).
	maxTopLevelBoxes = 512
	// maxMoovChildren bounds the one-level descent used to locate mvhd.
	maxMoovChildren = 128
)

type mp4Box struct {
	fourcc     string
	start, end int64 // inclusive box extent, header included
	dataStart  int64 // first byte after the (possibly extended) header
}

// probeMP4 walks the top-level box tree and maps ftyp/moov as critical and
// everything else (mdat, free, skip, udta, meta, moof, sidx, ...) as payload:
// players resolve samples via moov's absolute offsets, so losing non-moov
// bytes glitches playback rather than breaking it.
func probeMP4(ctx context.Context, r io.ReaderAt, fileSize int64) (*Structure, error) {
	var boxes []mp4Box
	pos := int64(0)
	for pos < fileSize {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(boxes) >= maxTopLevelBoxes {
			return nil, fmt.Errorf("mp4: more than %d top-level boxes", maxTopLevelBoxes)
		}
		box, err := readMP4BoxHeader(r, pos, fileSize)
		if err != nil {
			return nil, err
		}
		boxes = append(boxes, box)
		pos = box.end + 1
	}

	s := &Structure{Container: "mp4", FileSize: fileSize}
	sawFtyp, sawMoov := false, false
	for _, b := range boxes {
		br := ByteRange{Start: b.start, End: b.end, Label: b.fourcc}
		switch b.fourcc {
		case "ftyp", "styp":
			sawFtyp = true
			s.Critical = append(s.Critical, br)
		case "moov":
			sawMoov = true
			s.Critical = append(s.Critical, br)
		case "sidx":
			s.SeekOnly = append(s.SeekOnly, br)
		default:
			s.Payload = append(s.Payload, br)
		}
	}
	if !sawFtyp || !sawMoov {
		return nil, fmt.Errorf("mp4: missing required top-level boxes (ftyp=%v moov=%v)", sawFtyp, sawMoov)
	}

	for _, b := range boxes {
		if b.fourcc == "moov" {
			if dur, err := readMP4Duration(r, b); err == nil {
				s.DurationSeconds = dur
			}
			break
		}
	}
	return s, nil
}

// readMP4BoxHeader reads and validates one box header at pos.
func readMP4BoxHeader(r io.ReaderAt, pos, fileSize int64) (mp4Box, error) {
	var hdr [8]byte
	if _, err := r.ReadAt(hdr[:], pos); err != nil {
		return mp4Box{}, fmt.Errorf("mp4: read box header at %d: %w", pos, err)
	}
	size := int64(binary.BigEndian.Uint32(hdr[:4]))
	fourcc := string(hdr[4:8])
	if !isPrintableFourCC(fourcc) {
		return mp4Box{}, fmt.Errorf("mp4: invalid box type %q at %d", fourcc, pos)
	}
	dataStart := pos + 8
	switch size {
	case 0: // box extends to end of file
		size = fileSize - pos
	case 1: // 64-bit largesize follows
		var ext [8]byte
		if _, err := r.ReadAt(ext[:], pos+8); err != nil {
			return mp4Box{}, fmt.Errorf("mp4: read largesize at %d: %w", pos+8, err)
		}
		size = int64(binary.BigEndian.Uint64(ext[:]))
		dataStart = pos + 16
		if size < 16 {
			return mp4Box{}, fmt.Errorf("mp4: invalid largesize %d at %d", size, pos)
		}
	default:
		if size < 8 {
			return mp4Box{}, fmt.Errorf("mp4: invalid box size %d at %d", size, pos)
		}
	}
	if pos+size > fileSize {
		return mp4Box{}, fmt.Errorf("mp4: box %q at %d exceeds file size", fourcc, pos)
	}
	return mp4Box{fourcc: fourcc, start: pos, end: pos + size - 1, dataStart: dataStart}, nil
}

func isPrintableFourCC(s string) bool {
	if len(s) != 4 {
		return false
	}
	for _, c := range []byte(s) {
		// fourcc bytes are ASCII; the odd one out is the copyright sign
		// (0xA9) used by QuickTime metadata atoms.
		if (c < 0x20 || c > 0x7E) && c != 0xA9 {
			return false
		}
	}
	return true
}

// readMP4Duration descends one level into moov to find mvhd and returns the
// presentation duration in seconds.
func readMP4Duration(r io.ReaderAt, moov mp4Box) (float64, error) {
	pos := moov.dataStart
	for n := 0; pos < moov.end && n < maxMoovChildren; n++ {
		child, err := readMP4BoxHeader(r, pos, moov.end+1)
		if err != nil {
			return 0, err
		}
		if child.fourcc == "mvhd" {
			var vf [4]byte
			if _, err := r.ReadAt(vf[:], child.dataStart); err != nil {
				return 0, err
			}
			switch vf[0] {
			case 0:
				var buf [8]byte // timescale(4) + duration(4)
				if _, err := r.ReadAt(buf[:], child.dataStart+12); err != nil {
					return 0, err
				}
				timescale := binary.BigEndian.Uint32(buf[:4])
				duration := binary.BigEndian.Uint32(buf[4:])
				if timescale == 0 {
					return 0, fmt.Errorf("mp4: mvhd timescale is zero")
				}
				return float64(duration) / float64(timescale), nil
			case 1:
				var buf [12]byte // timescale(4) + duration(8)
				if _, err := r.ReadAt(buf[:], child.dataStart+20); err != nil {
					return 0, err
				}
				timescale := binary.BigEndian.Uint32(buf[:4])
				duration := binary.BigEndian.Uint64(buf[4:])
				if timescale == 0 {
					return 0, fmt.Errorf("mp4: mvhd timescale is zero")
				}
				return float64(duration) / float64(timescale), nil
			default:
				return 0, fmt.Errorf("mp4: unsupported mvhd version %d", vf[0])
			}
		}
		pos = child.end + 1
	}
	return 0, fmt.Errorf("mp4: mvhd not found in moov")
}
