package mediaprobe

import (
	"context"
	"fmt"
	"io"

	mp4 "github.com/abema/go-mp4"
)

// maxTopLevelBoxes bounds the walk; real files have well under 100
// top-level boxes (fragmented MP4s with thousands of moof boxes are
// aborted rather than walked exhaustively).
const maxTopLevelBoxes = 512

// probeMP4 walks the top-level box tree (via github.com/abema/go-mp4, which
// handles box-header quirks — largesize, extends-to-EOF, versioned boxes —
// so this code only decides what each box means) and maps ftyp/moov as
// critical and everything else (mdat, free, skip, udta, meta, moof, sidx as
// seek-only, ...) as payload: players resolve samples via moov's absolute
// offsets, so losing non-moov bytes glitches playback rather than breaking it.
func probeMP4(ctx context.Context, r io.ReaderAt, fileSize int64) (*Structure, error) {
	sr := io.NewSectionReader(r, 0, fileSize)

	s := &Structure{Container: "mp4", FileSize: fileSize}
	sawFtyp, sawMoov := false, false
	topLevelCount := 0

	_, err := mp4.ReadBoxStructure(sr, func(h *mp4.ReadHandle) (any, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		// h.Path includes the current box as its last element, so a
		// top-level box has len(h.Path) == 1 and a direct child of moov has
		// len(h.Path) == 2 with h.Path[0] == moov.
		//
		// Locate mvhd (the one child of moov we care about) to read the
		// movie duration. Everything else under moov (trak sample tables,
		// udta, ...) is left unexpanded: we only need moov's own byte range,
		// not its payload.
		if len(h.Path) == 2 && h.Path[0] == mp4.BoxTypeMoov() && h.BoxInfo.Type == mp4.BoxTypeMvhd() {
			box, _, err := h.ReadPayload()
			if err != nil {
				return nil, fmt.Errorf("mp4: read mvhd: %w", err)
			}
			if mvhd, ok := box.(*mp4.Mvhd); ok && mvhd.Timescale != 0 {
				s.DurationSeconds = float64(mvhd.GetDuration()) / float64(mvhd.Timescale)
			}
			return nil, nil
		}
		if len(h.Path) != 1 {
			return nil, nil // not top-level, and not moov/mvhd above — skip
		}

		// Top-level box.
		topLevelCount++
		if topLevelCount > maxTopLevelBoxes {
			return nil, fmt.Errorf("mp4: more than %d top-level boxes", maxTopLevelBoxes)
		}
		fourcc := h.BoxInfo.Type.String()
		if !isPrintableFourCC(fourcc) {
			return nil, fmt.Errorf("mp4: invalid box type %q at %d", fourcc, h.BoxInfo.Offset)
		}

		start := int64(h.BoxInfo.Offset)
		end := start + int64(h.BoxInfo.Size) - 1
		if h.BoxInfo.ExtendToEOF {
			end = fileSize - 1
		}
		if end >= fileSize || end < start {
			return nil, fmt.Errorf("mp4: box %q at %d exceeds file size", fourcc, start)
		}
		br := ByteRange{Start: start, End: end, Label: fourcc}

		switch fourcc {
		case "ftyp", "styp":
			sawFtyp = true
			s.Critical = append(s.Critical, br)
			return nil, nil
		case "moov":
			sawMoov = true
			s.Critical = append(s.Critical, br)
			return h.Expand() // descend one level to find mvhd
		case "sidx":
			s.SeekOnly = append(s.SeekOnly, br)
		default:
			s.Payload = append(s.Payload, br)
		}
		return nil, nil
	})
	if err != nil {
		return nil, fmt.Errorf("mp4: %w", err)
	}
	if !sawFtyp || !sawMoov {
		return nil, fmt.Errorf("mp4: missing required top-level boxes (ftyp=%v moov=%v)", sawFtyp, sawMoov)
	}
	return s, nil
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
