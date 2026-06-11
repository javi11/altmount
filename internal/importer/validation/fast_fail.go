package validation

import (
	"context"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/usenet"
	concpool "github.com/sourcegraph/conc/pool"
)

// selectFastFailSegments picks a lightweight per-file sample for the fast-fail
// reachability gate: always the first and last segment (DMCA/truncation
// detection) plus samplePercentage% of the middle, capped at 55 to bound very
// large files. It is intentionally lighter than usenet.SelectSegmentsForValidation
// (which health checks use and which floors at 5 per file): fast-fail Stats run
// across every file in the NZB, so a min-5 floor multiplies badly on multi-part
// releases.
func selectFastFailSegments(segments []*metapb.SegmentData, samplePercentage int) []*metapb.SegmentData {
	n := len(segments)
	if n <= 2 {
		return segments
	}

	const maxSamples = 55

	chosen := make(map[int]struct{}, maxSamples)
	out := make([]*metapb.SegmentData, 0, maxSamples)
	add := func(i int) {
		if _, ok := chosen[i]; ok {
			return
		}
		chosen[i] = struct{}{}
		out = append(out, segments[i])
	}

	add(0)     // first — catches whole-article DMCA takedowns / missing files
	add(n - 1) // last — catches truncated/incomplete uploads

	middleCount := (n * samplePercentage) / 100
	if len(out)+middleCount > maxSamples {
		middleCount = maxSamples - len(out)
	}
	if middleCount > 0 {
		middleRange := n - 2 // sample from indices [1, n-2]
		perm := rand.Perm(middleRange)
		for i := 0; i < middleCount && i < len(perm); i++ {
			add(1 + perm[i])
		}
	}

	return out
}

// FastFailFile is the minimal file surface needed for early segment reachability checks.
type FastFailFile struct {
	Filename string
	Segments []*metapb.SegmentData
	// GroupKey identifies the multi-volume set this file belongs to (e.g. a RAR
	// base name). Empty means the file is standalone. When any member of a group
	// is found unreachable, FastFailCheckFiles skips the remaining Stats for that
	// group and marks every member Broken — a missing volume dooms the whole set
	// (no PAR2 repair at import time), so probing the rest is wasted work.
	GroupKey string
}

// FastFailReleaseProbe is the cheap phase-1 reachability gate for an NZB import.
// It flattens all candidate segments across the release and Stats a single
// sample (usenet.SelectSegmentsForValidation: first 3 + last 2 + random middle,
// min 5 / max 55 for the whole release), cancelling the remaining Stats on the
// first miss.
//
// Returns (missing, err):
//   - err is reserved for infrastructure failures (pool unavailable/nil).
//   - missing reports whether any sampled segment was unreachable. A 430 / Stat
//     failure / timeout yields (true, nil) — not an error — so the caller can
//     escalate to the per-file FastFailCheckFiles sweep to map exactly which
//     files are broken. A clean release returns (false, nil) and the caller
//     proceeds straight to parsing, paying only this sample's worth of Stats.
func FastFailReleaseProbe(
	ctx context.Context,
	files []FastFailFile,
	poolManager pool.Manager,
	segmentSamplePercentage int,
	maxConnections int,
	timeout time.Duration,
) (bool, error) {
	var segments []*metapb.SegmentData
	for _, file := range files {
		for _, segment := range file.Segments {
			if segment != nil && segment.Id != "" {
				segments = append(segments, segment)
			}
		}
	}
	if len(segments) == 0 {
		return false, nil
	}

	selected := usenet.SelectSegmentsForValidation(segments, segmentSamplePercentage)
	if len(selected) == 0 {
		return false, nil
	}

	if !poolManager.HasPool() {
		return false, fmt.Errorf("cannot fast-fail import: usenet connection pool is nil")
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return false, fmt.Errorf("cannot fast-fail import: usenet connection pool unavailable: %w", err)
	}
	if usenetPool == nil {
		return false, fmt.Errorf("cannot fast-fail import: usenet connection pool is nil")
	}

	if maxConnections <= 0 {
		maxConnections = 1
	}

	// Stat the sample concurrently, cancelling the rest on the first miss.
	// Infrastructure failures are handled above, so any error returned by a
	// goroutine here indicates an unreachable segment.
	pl := concpool.New().WithContext(ctx).WithCancelOnError().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, seg := range selected {
		pl.Go(func(ctx context.Context) error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			if _, err := usenetPool.Stat(checkCtx, seg.Id); err != nil {
				return err
			}
			return nil
		})
	}

	if err := pl.Wait(); err != nil {
		return true, nil
	}
	return false, nil
}

// FastFailFileResult records the reachability outcome for a single FastFailFile.
// Results from FastFailCheckFiles are index-aligned with the input slice.
type FastFailFileResult struct {
	Broken            bool
	MissingSegmentIDs []string // segment IDs whose Stat failed
}

// FastFailCheckFiles stats a per-file sample of segments from all files.
// Every file with segments is checked — broken files are excluded from
// parsing, and if only PAR2 files survive the import fails naturally. Pass
// nil Segments for files that should be skipped (e.g. PAR2/sidecars) to keep
// index alignment while avoiding wasted Stat round-trips.
// Returns one result per input file (index-aligned). Files with no segments
// are skipped. Infrastructure failures (pool unavailable) are returned as an
// error; per-segment Stat failures mark the owning file Broken. progressTracker
// may be nil; when set it reports completed Stats as work progresses.
func FastFailCheckFiles(
	ctx context.Context,
	files []FastFailFile,
	poolManager pool.Manager,
	segmentSamplePercentage int,
	maxConnections int,
	timeout time.Duration,
	progressTracker progress.ProgressTracker,
) ([]FastFailFileResult, error) {
	if !poolManager.HasPool() {
		return nil, fmt.Errorf("cannot fast-fail import: usenet connection pool is nil")
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return nil, fmt.Errorf("cannot fast-fail import: usenet connection pool unavailable: %w", err)
	}

	if maxConnections <= 0 {
		maxConnections = 1
	}

	results := make([]FastFailFileResult, len(files))
	var mu sync.Mutex

	// brokenGroups records group keys with at least one unreachable segment, so
	// remaining Stats for those groups can be skipped. Guarded by mu.
	brokenGroups := make(map[string]struct{})

	// Build the flat work list first so we know the total up front for progress.
	type statJob struct {
		fileIdx  int
		segID    string
		groupKey string
	}

	// Select each file's sample once, then interleave the jobs round-robin
	// across files (every file's first sample, then every file's second, …).
	// File-by-file ordering would Stat all of a broken set's parts before any
	// sibling, defeating the group short-circuit; round-robin makes the first
	// miss of a set land within roughly len(files) Stats so siblings are
	// skipped. Per-file selection already places Segments[0] first.
	perFile := make([][]*metapb.SegmentData, len(files))
	maxSamples := 0
	for fileIdx, file := range files {
		if len(file.Segments) == 0 {
			continue
		}
		perFile[fileIdx] = selectFastFailSegments(file.Segments, segmentSamplePercentage)
		if len(perFile[fileIdx]) > maxSamples {
			maxSamples = len(perFile[fileIdx])
		}
	}

	var jobs []statJob
	for round := 0; round < maxSamples; round++ {
		for fileIdx, selected := range perFile {
			if round < len(selected) {
				jobs = append(jobs, statJob{
					fileIdx:  fileIdx,
					segID:    selected[round].Id,
					groupKey: files[fileIdx].GroupKey,
				})
			}
		}
	}

	total := len(jobs)
	if total == 0 {
		return results, nil
	}

	var done int32
	var lastPct int32 = -1
	pl := concpool.New().WithMaxGoroutines(maxConnections)

	for _, job := range jobs {
		pl.Go(func() {
			// Skip the Stat if this job's group is already known broken — the
			// whole set is doomed, so reachability of the rest is irrelevant.
			// Still advance progress so the bar completes.
			skip := false
			if job.groupKey != "" {
				mu.Lock()
				_, skip = brokenGroups[job.groupKey]
				mu.Unlock()
			}

			if !skip {
				checkCtx, cancel := context.WithTimeout(ctx, timeout)
				defer cancel()

				if _, statErr := usenetPool.Stat(checkCtx, job.segID); statErr != nil {
					mu.Lock()
					results[job.fileIdx].Broken = true
					results[job.fileIdx].MissingSegmentIDs = append(results[job.fileIdx].MissingSegmentIDs, job.segID)
					if job.groupKey != "" {
						brokenGroups[job.groupKey] = struct{}{}
					}
					mu.Unlock()
				}
			}

			// Emit progress only when the integer percentage advances, to avoid
			// hundreds of redundant broadcasts during a large sweep. The benign
			// race on lastPct can at worst cause a couple of extra updates.
			if progressTracker != nil {
				d := atomic.AddInt32(&done, 1)
				pct := d * 100 / int32(total)
				if pct != atomic.LoadInt32(&lastPct) {
					atomic.StoreInt32(&lastPct, pct)
					progressTracker.Update(int(d), total)
				}
			}
		})
	}

	pl.Wait()

	// Propagate set breakage: every file in a broken group is marked Broken so
	// the entire doomed set is excluded from parsing as one unit. Siblings carry
	// no synthetic MissingSegmentIDs — only segments actually observed missing
	// are reported.
	if len(brokenGroups) > 0 {
		for i := range files {
			if files[i].GroupKey == "" || results[i].Broken {
				continue
			}
			if _, broken := brokenGroups[files[i].GroupKey]; broken {
				results[i].Broken = true
			}
		}
	}

	return results, nil
}
