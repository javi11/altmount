package importer

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/javi11/nntppool/v5"
	"github.com/javi11/rardecode/v2"

	"github.com/kipsilabs/altmount/internal/config"
	alterrors "github.com/kipsilabs/altmount/internal/errors"
	"github.com/kipsilabs/altmount/internal/importer/multifile"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
)

// A damaged archive set with PAR2 files defers for repair instead of being
// dropped, when repair_on_import is on. Archive volumes cannot be zero-filled,
// but PAR2 can reconstruct them exactly — so dropping them is a lost repair.
func TestDeferDecision(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		hasPar2     bool
		brokenInSet bool
		want        bool
	}{
		{"damaged set with par2 and feature on defers", true, true, true, true},
		{"feature off drops as before", false, true, true, false},
		{"no par2 files: nothing to repair with", true, false, true, false},
		{"nothing broken: normal import", true, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDeferForRepair(tt.enabled, tt.hasPar2, tt.brokenInSet)
			if got != tt.want {
				t.Fatalf("shouldDeferForRepair(%v,%v,%v) = %v, want %v",
					tt.enabled, tt.hasPar2, tt.brokenInSet, got, tt.want)
			}
		})
	}
}

// The deferral surfaces as a distinct sentinel so the service can park the
// queue item rather than failing it.
func TestErrDeferredForRepairIsDistinct(t *testing.T) {
	err := ErrDeferredForRepair
	if !errors.Is(err, ErrDeferredForRepair) {
		t.Fatal("sentinel must match itself")
	}
	if errors.Is(err, ErrArticlesNotFound) {
		t.Fatal("deferral must not be mistaken for a normal missing-articles failure")
	}
}

// A release where EVERY file is damaged is the canonical deferral case — a
// fully-holed archive set. The decision must be reached before the
// "nothing healthy remains to import" bail-out, or deferral never fires for
// the exact releases it exists to rescue.
func TestDeferPrecedesNothingProcessedBailout(t *testing.T) {
	// allBroken mirrors the sweep's terminal state: every eligible file broken.
	const eligible, broken = 112, 112
	deferred, bail := fastFailOutcome(
		true, /* repair_on_import */
		true, /* nzb has par2 */
		true, /* damage is inside an archive set */
		eligible, broken,
	)
	if !deferred {
		t.Fatal("a fully damaged archive set with PAR2 must defer for repair")
	}
	if bail {
		t.Fatal("deferral must take precedence over the no-files-processed bail-out")
	}
}

// A corrupt-but-present article breaks archive analysis with a rardecode
// corruption error after the fast-fail sweep (which only sees MISSING
// articles) passed clean. That damage is exactly what PAR2 exists for, so the
// import defers for a repair instead of failing — when the NZB carries PAR2
// files and repair-on-import is enabled.
func TestDeferCorruptArchiveDecision(t *testing.T) {
	// The exact composition the pipeline produces: the aggregator joins the
	// RAR processor's NonRetryableError wrapping the rardecode sentinel.
	corruption := errors.Join(alterrors.NewNonRetryableError(
		`failed to iterate RAR archive "a.part01.rar"`, rardecode.ErrBadHeaderCRC))
	other := errors.New("failed to write metadata")

	tests := []struct {
		name    string
		err     error
		enabled bool
		hasPar2 bool
		want    bool
	}{
		{"corruption with par2 and feature on defers", corruption, true, true, true},
		{"feature off fails as before", corruption, false, true, false},
		{"no par2 files: nothing to repair with", corruption, true, false, false},
		{"non-corruption errors fail as before", other, true, true, false},
		{"nil error never defers", nil, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDeferCorruptArchive(tt.err, tt.enabled, tt.hasPar2)
			if got != tt.want {
				t.Fatalf("shouldDeferCorruptArchive(%v,%v,%v) = %v, want %v",
					tt.err, tt.enabled, tt.hasPar2, got, tt.want)
			}
		})
	}
}

// The fast-fail probe only samples a percentage of segments, so a release
// with a genuinely missing article can still pass it clean; the miss then
// surfaces later as an ErrArticleNotFound once analysis walks into the hole.
// That is the same damage the fast-fail escalation path defers for, so it
// must not be dropped as a terminal failure just because it was discovered
// during analysis instead of during the sweep.
func TestDeferMissingArchiveDecision(t *testing.T) {
	missing := errors.Join(alterrors.NewNonRetryableError(
		`failed to iterate RAR archive "a.part01.rar"`, nntppool.ErrArticleNotFound))
	other := errors.New("failed to write metadata")

	tests := []struct {
		name    string
		err     error
		enabled bool
		hasPar2 bool
		want    bool
	}{
		{"missing article with par2 and feature on defers", missing, true, true, true},
		{"feature off fails as before", missing, false, true, false},
		{"no par2 files: nothing to repair with", missing, true, false, false},
		{"non-missing-article errors fail as before", other, true, true, false},
		{"nil error never defers", nil, true, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDeferMissingArchive(tt.err, tt.enabled, tt.hasPar2)
			if got != tt.want {
				t.Fatalf("shouldDeferMissingArchive(%v,%v,%v) = %v, want %v",
					tt.err, tt.enabled, tt.hasPar2, got, tt.want)
			}
		})
	}
}

// Without PAR2 there is nothing to repair from, so the bail-out still wins.
func TestBailoutWhenNothingToRepairFrom(t *testing.T) {
	deferred, bail := fastFailOutcome(true, false, true, 112, 112)
	if deferred {
		t.Fatal("must not defer without PAR2 files")
	}
	if !bail {
		t.Fatal("a fully damaged release with no PAR2 must still bail out")
	}
}

// A header recovered from a later article marks the file degraded in the parser;
// that must reach the repair queue alongside the fast-fail sweep's own findings.
func TestMergeDegradedFilesCombinesSweepAndParserFindings(t *testing.T) {
	sweep := map[string]string{"a.mkv": "seg-a"}
	fromParser := map[string]string{"b.mkv": "seg-b", "a.mkv": "seg-a2"}

	got := mergeDegradedFiles(sweep, fromParser)

	if got["a.mkv"] != "seg-a" {
		t.Errorf("sweep finding must win for a.mkv, got %q", got["a.mkv"])
	}
	if got["b.mkv"] != "seg-b" {
		t.Errorf("parser finding missing for b.mkv, got %q", got["b.mkv"])
	}
	if nilMerge := mergeDegradedFiles(nil, map[string]string{"c.mkv": "seg-c"}); nilMerge["c.mkv"] != "seg-c" {
		t.Errorf("merging into nil map lost c.mkv: %+v", nilMerge)
	}
	if empty := mergeDegradedFiles(nil, nil); empty != nil {
		t.Errorf("merging two nil maps should stay nil, got %+v", empty)
	}
}

// With stremio.skip_repair, Stremio imports never queue or wait for a repair;
// other imports follow repair_on_import alone.
func TestRepairOnImportStremioSkipRepair(t *testing.T) {
	on, off := true, false
	stremioCat, tvCat := "stremio", "tv"
	stremioID, sabID := "stremio:abc", "SABnzbd_nzo_1"
	tests := []struct {
		name       string
		skip       *bool
		category   *string
		downloadID *string
		want       bool
	}{
		{"library import, skip on", &on, &tvCat, &sabID, true},
		{"stremio download id, skip on", &on, &tvCat, &stremioID, false},
		{"stremio category, skip on", &on, &stremioCat, nil, false},
		{"stremio import, skip off", &off, &tvCat, &stremioID, true},
		{"stremio import, skip unset", nil, &tvCat, &stremioID, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := config.DefaultConfig()
			cfg.Par2Repair.Enabled = &on
			cfg.Par2Repair.RepairOnImport = &on
			cfg.Stremio.SkipRepair = tt.skip
			if got := repairOnImport(cfg, tt.category, tt.downloadID); got != tt.want {
				t.Fatalf("repairOnImport = %v, want %v", got, tt.want)
			}
		})
	}

	cfg := config.DefaultConfig()
	cfg.Stremio.SkipRepair = &on
	if repairOnImport(cfg, &tvCat, &sabID) {
		t.Fatal("repair_on_import off must still disable repair")
	}
}

// A damaged archive set that would park for repair fails instead when it is a
// Stremio import and stremio.skip_repair is on, so playback can fall back.
func TestPreParseFastFailStremioSkipRepairFailsInsteadOfDeferring(t *testing.T) {
	for _, skip := range []bool{false, true} {
		t.Run(fmt.Sprintf("skip_repair=%v", skip), func(t *testing.T) {
			client := fakepool.New()
			client.SetBehavior("a2-missing", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
			proc := &Processor{
				poolManager:       processorTestPoolManager{client: client},
				validationTimeout: 100 * time.Millisecond,
			}
			n := buildTestNzb([]testNzbFile{
				{name: "Show.S01E01.part01.rar", segID: "a1"},
				{name: "Show.S01E01.part02.rar", segID: "a2-missing"},
				{name: "Show.S01E01.par2", segID: "p1"},
			})
			on, off := true, false
			cfg := config.DefaultConfig()
			cfg.Import.SegmentSamplePercentage = 100
			cfg.Par2Repair.Enabled = &on
			cfg.Par2Repair.RepairOnImport = &on
			cfg.Stremio.FastFailHeaderOnly = &off
			cfg.Stremio.SkipRepair = &skip
			downloadID := "stremio:abc"

			_, _, _, err := proc.preParseFastFail(context.Background(), n, cfg, 1, nil, &downloadID)
			var deferred *DeferredRepairError
			if skip {
				if !errors.Is(err, multifile.ErrNoFilesProcessed) {
					t.Fatalf("err = %v, want ErrNoFilesProcessed", err)
				}
				return
			}
			if !errors.As(err, &deferred) {
				t.Fatalf("err = %v, want DeferredRepairError", err)
			}
		})
	}
}
