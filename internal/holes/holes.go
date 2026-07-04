// Package holes is the shared model + threshold policy for "holes": runs of
// consecutive segments confirmed missing from Usenet providers. One table
// governs every moment the engine meets a hole:
//
//   - at IMPORT, the fast-fail sweep classifies a file's sampled damage
//     (fail the import / import as degraded / keep checking),
//   - at HEALTH CHECK, sampled or full sweeps classify accumulated damage, and
//   - at PLAYBACK, the hole hooks decide per miss whether to zero-fill and
//     keep streaming or to kill the stream and fail the file.
//
// Ported from AIOStreams' holes.ts. This package is imported by usenet,
// importer, health and nzbfilesystem layers; it must stay dependency-free.
package holes

import (
	"path/filepath"
	"strings"
)

const (
	// MaxPadRunSegments is the longest run of consecutive missing segments
	// that may be zero-filled. A longer measured run fails the file.
	MaxPadRunSegments = 4
	// MaxPadTotalSegments is the cumulative padded-segment cap per file.
	MaxPadTotalSegments = 64
	// MaxPadFileBytesRatio is the cumulative padded-bytes share of the file
	// above which it is unwatchable. The segment-count caps are the primary
	// guards; this ratio only protects small files where the segment caps
	// would be a large share.
	MaxPadFileBytesRatio = 0.02
	// ProjectionMinHits is the minimum number of confirmed misses a PARTIAL
	// sample needs before a projection may fail a file; an observed run
	// beyond MaxPadRunSegments fails regardless (measured, not projected).
	ProjectionMinHits = 8
	// ProjectionMargin is how far a projection must exceed the cumulative cap
	// to fail early from partial evidence.
	ProjectionMargin = 2
)

// Run is a run of consecutive missing segments in one file's segment space.
type Run struct {
	// Start is the first missing segment index.
	Start int
	// Count is the number of consecutive missing segments.
	Count int
}

// Verdict classifies a file's confirmed hole damage.
type Verdict string

const (
	// VerdictClean means no confirmed damage (only a FULL check can prove it).
	VerdictClean Verdict = "clean"
	// VerdictDegraded means damage within the padding caps: playable with
	// glitches, zero-filled during streaming.
	VerdictDegraded Verdict = "degraded"
	// VerdictFailed means damage beyond the caps: unwatchable.
	VerdictFailed Verdict = "failed"
	// VerdictUnknown means a partial sample found nothing — absence of
	// evidence, not evidence of absence.
	VerdictUnknown Verdict = "unknown"
)

// Decision is the playback hook's verdict for one confirmed missing segment.
type Decision string

const (
	// DecisionPad zero-fills the segment and keeps streaming.
	DecisionPad Decision = "pad"
	// DecisionFail kills the stream (caps exceeded / ineligible).
	DecisionFail Decision = "fail"
)

// Impact is the serializable playback-impact summary persisted in health
// error details (playback_impact key) and rendered by the frontend.
type Impact struct {
	Verdict Verdict `json:"verdict"`
	// TotalMissing is the number of confirmed missing segments (capped
	// captures may undercount; Verdict already accounts for that).
	TotalMissing int `json:"total_missing"`
	// LongestRun is the longest observed run of consecutive missing segments.
	LongestRun int `json:"longest_run"`
	// Sampled is how many segments the sweep checked (0 = playback discovery).
	Sampled int `json:"sampled,omitempty"`
	// TotalSegments is the file's full segment count.
	TotalSegments int `json:"total_segments,omitempty"`
	// PaddedRatio is missing bytes / file bytes, when the file size is known.
	PaddedRatio float64 `json:"padded_ratio,omitempty"`
}

// padEligibleExtensions are the video containers whose payload tolerates
// zeroed ranges (decoder glitches and resyncs). Zero-filling anything else
// would silently corrupt copies/imports served through the mount.
var padEligibleExtensions = map[string]struct{}{
	".mp4":  {},
	".m4v":  {},
	".mov":  {},
	".mkv":  {},
	".webm": {},
}

// EligibleFile reports whether a file may have missing segments zero-filled,
// based on its extension.
func EligibleFile(name string) bool {
	_, ok := padEligibleExtensions[strings.ToLower(filepath.Ext(name))]
	return ok
}

// Classify applies the threshold table to a file's confirmed damage. runs
// must cover the file's whole segment space (i.e. come from a full check or
// the persisted hole map). fileBytes is the file's decoded size when known
// (<= 0 skips the ratio guard); avgSegBytes an average decoded segment size
// (encoded sizes are fine; yEnc overhead is negligible for the ratio guard).
func Classify(runs []Run, fileBytes int64, avgSegBytes int64) Verdict {
	if len(runs) == 0 {
		return VerdictClean
	}
	total := 0
	longest := 0
	for _, r := range runs {
		total += r.Count
		if r.Count > longest {
			longest = r.Count
		}
	}
	if longest > MaxPadRunSegments {
		return VerdictFailed
	}
	if total > MaxPadTotalSegments {
		return VerdictFailed
	}
	if fileBytes > 0 {
		if avgSegBytes < 1 {
			avgSegBytes = 1
		}
		if float64(total)*float64(avgSegBytes) > MaxPadFileBytesRatio*float64(fileBytes) {
			return VerdictFailed
		}
	}
	return VerdictDegraded
}

// ClassifyProjected classifies from a partial, uniform sample. It never
// returns VerdictClean: absence of evidence is VerdictUnknown until a full
// check completes.
func ClassifyProjected(hits, sampled, totalSegments, longestObservedRun int) Verdict {
	if longestObservedRun > MaxPadRunSegments {
		return VerdictFailed
	}
	if hits <= 0 {
		return VerdictUnknown
	}
	// Every sampled segment was missing: the file is almost certainly gone in
	// its entirety (this also fails degenerate single-segment files whose one
	// segment is missing), regardless of the absolute hit count.
	if sampled > 0 && hits >= sampled {
		return VerdictFailed
	}
	if hits >= ProjectionMinHits && sampled > 0 {
		fraction := float64(hits) / float64(sampled)
		projected := fraction * float64(totalSegments)
		// Fail if the projected missing segment count blows the cumulative
		// cap, or the projected miss fraction blows the byte-ratio cap
		// (catches heavily-holed small files the count cap would miss).
		if projected > ProjectionMargin*MaxPadTotalSegments || fraction > MaxPadFileBytesRatio {
			return VerdictFailed
		}
	}
	return VerdictDegraded
}

// Accumulator incrementally merges missing segments into maximal
// non-overlapping runs. Used by import/health sweeps (spread hits) and by
// playback sessions (pads as they happen, including out-of-order discovery
// via seeks). Not safe for concurrent use; callers hold their own lock.
type Accumulator struct {
	// runs are sorted by Start, non-overlapping, non-adjacent.
	runs    []Run
	total   int
	longest int
}

// Total is the total number of missing segments across all runs.
func (a *Accumulator) Total() int { return a.total }

// LongestRun is the length of the longest run ever merged. It never shrinks.
func (a *Accumulator) LongestRun() int { return a.longest }

// Runs returns all runs ordered by Start. The slice is a copy.
func (a *Accumulator) Runs() []Run {
	out := make([]Run, len(a.runs))
	copy(out, a.runs)
	return out
}

// Has reports whether the segment index lies inside a known run.
func (a *Accumulator) Has(index int) bool {
	for _, r := range a.runs {
		if index >= r.Start && index < r.Start+r.Count {
			return true
		}
	}
	return false
}

// Add records one missing segment, merging into adjacent runs.
func (a *Accumulator) Add(index int) {
	a.AddRun(Run{Start: index, Count: 1})
}

// AddRun records a measured run, merging with any overlapping or adjacent
// existing runs.
func (a *Accumulator) AddRun(run Run) {
	if run.Count <= 0 {
		return
	}
	newStart := run.Start
	newEnd := run.Start + run.Count // exclusive
	kept := a.runs[:0]
	for _, r := range a.runs {
		rEnd := r.Start + r.Count
		if rEnd < newStart || r.Start > newEnd {
			kept = append(kept, r)
		} else {
			a.total -= r.Count
			if r.Start < newStart {
				newStart = r.Start
			}
			if rEnd > newEnd {
				newEnd = rEnd
			}
		}
	}
	merged := Run{Start: newStart, Count: newEnd - newStart}
	// Insert keeping the sort by Start.
	pos := len(kept)
	for i, r := range kept {
		if r.Start > merged.Start {
			pos = i
			break
		}
	}
	kept = append(kept, Run{})
	copy(kept[pos+1:], kept[pos:])
	kept[pos] = merged
	a.runs = kept
	a.total += merged.Count
	if merged.Count > a.longest {
		a.longest = merged.Count
	}
}

// Load seeds the accumulator from persisted runs.
func (a *Accumulator) Load(runs []Run) {
	for _, r := range runs {
		a.AddRun(r)
	}
}
