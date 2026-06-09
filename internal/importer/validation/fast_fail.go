package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	concpool "github.com/sourcegraph/conc/pool"
)

// fastFailSidecarExtensions are small companion files (subtitles, info, checksums,
// thumbnails, …) whose reachability isn't worth a network round-trip. Everything else —
// media, archives, and unknown content — is validated by the single beginning pass.
var fastFailSidecarExtensions = map[string]struct{}{
	".nfo": {}, ".txt": {}, ".srt": {}, ".sub": {}, ".idx": {}, ".sfv": {},
	".md5": {}, ".par2": {}, ".jpg": {}, ".jpeg": {}, ".png": {}, ".gif": {},
	".nzb": {}, ".url": {}, ".diz": {}, ".bmp": {}, ".webp": {},
}

// FastFailFile is the minimal file surface needed for early segment reachability checks.
type FastFailFile struct {
	Filename string
	Segments []*metapb.SegmentData
}

// FastFailSegmentCheck stats a random sample of segments from eligible media/archive files.
// When disabled, no segments are checked. When enabled, segmentSamplePercentage
// uses the same selection strategy as regular segment validation.
func FastFailSegmentCheck(
	ctx context.Context,
	files []FastFailFile,
	poolManager pool.Manager,
	enabled bool,
	segmentSamplePercentage int,
	maxConnections int,
	timeout time.Duration,
) error {
	if !enabled {
		return nil
	}

	segments := collectFastFailSegments(files)
	if len(segments) == 0 {
		return nil
	}

	selected := usenet.SelectSegmentsForValidation(segments, segmentSamplePercentage)
	if len(selected) == 0 {
		return nil
	}

	usenetPool, err := poolManager.GetPool()
	if err != nil {
		return fmt.Errorf("cannot fast-fail import: usenet connection pool unavailable: %w", err)
	}
	if usenetPool == nil {
		return fmt.Errorf("cannot fast-fail import: usenet connection pool is nil")
	}

	if maxConnections <= 0 {
		maxConnections = 1
	}

	pl := concpool.New().WithErrors().WithFirstError().WithMaxGoroutines(maxConnections)
	for _, seg := range selected {
		pl.Go(func() error {
			checkCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			if _, err := usenetPool.Stat(checkCtx, seg.Id); err != nil {
				return fmt.Errorf("fast-fail segment with ID %s unreachable: %w", seg.Id, err)
			}
			return nil
		})
	}

	return pl.Wait()
}

func collectFastFailSegments(files []FastFailFile) []*metapb.SegmentData {
	var segments []*metapb.SegmentData
	for _, file := range files {
		if !isFastFailEligibleFile(file.Filename) {
			continue
		}
		for _, segment := range file.Segments {
			if segment != nil && segment.Id != "" {
				segments = append(segments, segment)
			}
		}
	}
	return segments
}

// isFastFailEligibleFile reports whether a file should be reachability-checked by the
// single beginning validation pass. Everything is eligible except known sidecar files.
func isFastFailEligibleFile(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	if base == "" {
		return false
	}
	ext := filepath.Ext(base)
	_, isSidecar := fastFailSidecarExtensions[ext]
	return !isSidecar
}
