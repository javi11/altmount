// Package mediaprobe maps the structural layout of video containers (MP4, MKV)
// and classifies whether missing byte ranges break playback. It operates only
// on io.ReaderAt so it can run over any storage backend and is fully testable
// without network access.
package mediaprobe

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// Verdict is the playback-impact outcome of classifying missing byte ranges.
type Verdict string

const (
	// VerdictFatal means playback is broken (a container-critical structure is lost).
	VerdictFatal Verdict = "fatal"
	// VerdictDegraded means the file still plays with a glitch or lost seeking.
	VerdictDegraded Verdict = "degraded"
	// VerdictUnknown means impact could not be determined; callers must treat it as fatal.
	VerdictUnknown Verdict = "unknown"
)

// ErrMissingRange is returned by caller-provided readers when a read overlaps
// a byte range known to be missing. The probe treats it as "critical region
// unreadable" and gives up with VerdictUnknown instead of hitting the network.
var ErrMissingRange = errors.New("mediaprobe: read overlaps missing range")

// ByteRange is an inclusive byte range in file coordinates.
type ByteRange struct {
	Start int64  `json:"start"`
	End   int64  `json:"end"`
	Label string `json:"label,omitempty"` // e.g. "moov", "mdat", "Tracks"
}

// TimeRange is an estimated playback window in seconds.
type TimeRange struct {
	FromSec float64 `json:"from_sec"`
	ToSec   float64 `json:"to_sec"`
}

// Structure is the serializable container map produced by one probe pass.
// Critical ranges break playback when lost; Payload ranges cause a transient
// glitch; SeekOnly ranges break seeking but not playback.
type Structure struct {
	Container       string      `json:"container"` // "mp4" | "mkv"
	FileSize        int64       `json:"file_size"`
	DurationSeconds float64     `json:"duration_seconds,omitempty"`
	Critical        []ByteRange `json:"critical"`
	Payload         []ByteRange `json:"payload"`
	SeekOnly        []ByteRange `json:"seek_only,omitempty"`
}

// Classification is the result of intersecting missing ranges with a Structure.
type Classification struct {
	Verdict         Verdict     `json:"verdict"`
	Container       string      `json:"container,omitempty"`
	Reason          string      `json:"reason"`
	MissingRanges   []ByteRange `json:"missing_ranges,omitempty"`
	AffectedTime    []TimeRange `json:"affected_time,omitempty"`
	DurationSeconds float64     `json:"duration_seconds,omitempty"`
}

// maxMissingForDegraded caps how many distinct missing ranges can still be
// called "degraded"; heavily holed files are fatal regardless of placement.
const maxMissingForDegraded = 20

// ContainerForFile returns the container kind implied by the file extension,
// or "" when the file is not a supported video type.
func ContainerForFile(fileName string) string {
	switch strings.ToLower(filepath.Ext(fileName)) {
	case ".mp4", ".m4v", ".mov":
		return "mp4"
	case ".mkv", ".webm":
		return "mkv"
	default:
		return ""
	}
}

// Probe walks the container's top-level structure and returns its map.
// It reads only small headers (never media payload); a read failure of any
// kind aborts the probe with an error.
func Probe(ctx context.Context, r io.ReaderAt, fileSize int64, fileName string) (*Structure, error) {
	if fileSize <= 0 {
		return nil, fmt.Errorf("mediaprobe: invalid file size %d", fileSize)
	}
	switch ContainerForFile(fileName) {
	case "mp4":
		return probeMP4(ctx, r, fileSize)
	case "mkv":
		return probeMKV(ctx, r, fileSize)
	default:
		return nil, fmt.Errorf("mediaprobe: unsupported container for %q", filepath.Base(fileName))
	}
}

// ClassifyAgainst intersects missing byte ranges with a previously probed
// Structure. It performs no I/O.
func ClassifyAgainst(s *Structure, missing []ByteRange) Classification {
	if s == nil {
		return Classification{Verdict: VerdictUnknown, Reason: "no container structure available"}
	}
	cls := Classification{
		Container:       s.Container,
		DurationSeconds: s.DurationSeconds,
		MissingRanges:   missing,
	}
	if len(missing) == 0 {
		cls.Verdict = VerdictUnknown
		cls.Reason = "no missing ranges provided"
		return cls
	}
	if len(missing) > maxMissingForDegraded {
		cls.Verdict = VerdictFatal
		cls.Reason = fmt.Sprintf("too much media data missing (%d ranges)", len(missing))
		return cls
	}

	seekOnlyHit := false
	for _, m := range missing {
		m = clampRange(m, s.FileSize)
		if m.Start > m.End {
			continue
		}
		if hit, label := overlapsAny(m, s.Critical); hit {
			cls.Verdict = VerdictFatal
			cls.Reason = fmt.Sprintf("missing range overlaps container-critical structure %s", labelOr(label, "header"))
			return cls
		}
		if !coveredBy(m, s.Payload, s.SeekOnly) {
			cls.Verdict = VerdictUnknown
			cls.Reason = "missing range falls outside the mapped container structure"
			return cls
		}
		if hit, _ := overlapsAny(m, s.SeekOnly); hit {
			seekOnlyHit = true
		}
	}

	cls.Verdict = VerdictDegraded
	cls.AffectedTime = estimateAffectedTime(s, missing)
	switch {
	case seekOnlyHit && len(cls.AffectedTime) == 0:
		cls.Reason = "missing range only affects the seek index; playback works, seeking may fail"
	case seekOnlyHit:
		cls.Reason = "missing ranges affect media payload and the seek index"
	default:
		cls.Reason = "missing ranges only intersect media payload; expect a short playback glitch"
	}
	return cls
}

// Classify probes the container live and classifies missing ranges against it.
// Any probe failure yields VerdictUnknown (callers treat it as fatal).
func Classify(ctx context.Context, r io.ReaderAt, fileSize int64, fileName string, missing []ByteRange) Classification {
	s, err := Probe(ctx, r, fileSize, fileName)
	if err != nil {
		return Classification{
			Verdict:       VerdictUnknown,
			Container:     ContainerForFile(fileName),
			Reason:        fmt.Sprintf("container probe failed: %v", err),
			MissingRanges: missing,
		}
	}
	return ClassifyAgainst(s, missing)
}

func labelOr(label, fallback string) string {
	if label != "" {
		return label
	}
	return fallback
}

func clampRange(m ByteRange, fileSize int64) ByteRange {
	if m.Start < 0 {
		m.Start = 0
	}
	if m.End >= fileSize {
		m.End = fileSize - 1
	}
	return m
}

func rangesOverlap(a, b ByteRange) bool {
	return a.Start <= b.End && b.Start <= a.End
}

func overlapsAny(m ByteRange, ranges []ByteRange) (bool, string) {
	for _, r := range ranges {
		if rangesOverlap(m, r) {
			return true, r.Label
		}
	}
	return false, ""
}

// coveredBy reports whether every byte of m lies within the union of the
// given range sets. Ranges may be unsorted and adjacent but must not be
// needed to overlap.
func coveredBy(m ByteRange, sets ...[]ByteRange) bool {
	var all []ByteRange
	for _, set := range sets {
		all = append(all, set...)
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Start < all[j].Start })
	cursor := m.Start
	for _, r := range all {
		if r.Start > cursor {
			if cursor > m.End {
				return true
			}
			if r.Start > m.End {
				break
			}
			return false
		}
		if r.End >= cursor {
			cursor = r.End + 1
		}
		if cursor > m.End {
			return true
		}
	}
	return cursor > m.End
}

// estimateAffectedTime converts payload byte positions into approximate
// playback windows using a byte-ratio model over the concatenated payload
// ranges. SeekOnly overlaps produce no time window.
func estimateAffectedTime(s *Structure, missing []ByteRange) []TimeRange {
	if s.DurationSeconds <= 0 {
		return nil
	}
	payload := append([]ByteRange(nil), s.Payload...)
	sort.Slice(payload, func(i, j int) bool { return payload[i].Start < payload[j].Start })
	var total int64
	cum := make([]int64, len(payload)) // payload-relative offset of each range start
	for i, p := range payload {
		cum[i] = total
		total += p.End - p.Start + 1
	}
	if total <= 0 {
		return nil
	}
	toSec := func(pos int64) float64 {
		return float64(pos) / float64(total) * s.DurationSeconds
	}

	var out []TimeRange
	for _, m := range missing {
		m = clampRange(m, s.FileSize)
		for i, p := range payload {
			if !rangesOverlap(m, p) {
				continue
			}
			start := max64(m.Start, p.Start)
			end := min64(m.End, p.End)
			out = append(out, TimeRange{
				FromSec: toSec(cum[i] + start - p.Start),
				ToSec:   toSec(cum[i] + end - p.Start + 1),
			})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].FromSec < out[j].FromSec })
	// Merge adjacent/overlapping windows.
	merged := out[:0]
	for _, t := range out {
		if n := len(merged); n > 0 && t.FromSec <= merged[n-1].ToSec+0.5 {
			if t.ToSec > merged[n-1].ToSec {
				merged[n-1].ToSec = t.ToSec
			}
			continue
		}
		merged = append(merged, t)
	}
	return merged
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
