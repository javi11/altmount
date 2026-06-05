package validation

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	concpool "github.com/sourcegraph/conc/pool"
)

var (
	fastFailRarPattern      = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
	fastFailSevenZipPattern = regexp.MustCompile(`(?i)\.7z$|\.7z\.\d+$`)
)

var fastFailEligibleExtensions = map[string]struct{}{
	".3g2": {}, ".3gp": {}, ".aac": {}, ".aif": {}, ".avi": {}, ".flac": {},
	".m2ts": {}, ".m4a": {}, ".m4b": {}, ".m4v": {}, ".mka": {}, ".mkv": {},
	".mov": {}, ".mp3": {}, ".mp4": {}, ".mpa": {}, ".mpeg": {}, ".mpg": {},
	".oga": {}, ".ogg": {}, ".ogv": {}, ".opus": {}, ".rm": {}, ".rmvb": {},
	".ts": {}, ".vob": {}, ".wav": {}, ".weba": {}, ".webm": {}, ".wma": {},
	".wmv": {}, ".asf": {}, ".asx": {}, ".dvr-ms": {}, ".mk3d": {}, ".wtv": {},
	".xvid": {},
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

func isFastFailEligibleFile(filename string) bool {
	base := strings.ToLower(filepath.Base(filename))
	if base == "" {
		return false
	}
	if fastFailRarPattern.MatchString(base) || fastFailSevenZipPattern.MatchString(base) {
		return true
	}
	ext := filepath.Ext(base)
	_, ok := fastFailEligibleExtensions[ext]
	return ok
}
