package importer

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/nzbgap"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nzbparser"
)

// gapTestNzb builds a RAR set where part02's NZB entry omits segment number 2
// (three declared segments, only numbers 1 and 3 listed), plus a PAR2 file.
func gapTestNzb() *nzbparser.Nzb {
	return &nzbparser.Nzb{Files: nzbparser.NzbFiles{
		{
			Filename: "setA.part01.rar",
			Subject:  "setA.part01.rar",
			Segments: nzbparser.NzbSegments{
				{Bytes: 100, Number: 1, ID: "a1-1"},
				{Bytes: 100, Number: 2, ID: "a1-2"},
				{Bytes: 100, Number: 3, ID: "a1-3"},
			},
		},
		{
			Filename: "setA.part02.rar",
			Subject:  "setA.part02.rar",
			Segments: nzbparser.NzbSegments{
				{Bytes: 100, Number: 1, ID: "a2-1"},
				{Bytes: 100, Number: 3, ID: "a2-3"},
			},
		},
		{
			Filename: "setA.par2",
			Subject:  "setA.par2",
			Segments: nzbparser.NzbSegments{
				{Bytes: 100, Number: 1, ID: "p1"},
			},
		},
	}}
}

func gapRepairConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Import.SegmentSamplePercentage = 100
	on := true
	cfg.Par2Repair.Enabled = &on
	cfg.Par2Repair.RepairOnImport = &on
	return cfg
}

type fakePatchIndex map[string]bool

func (f fakePatchIndex) Has(id string) bool { return f[id] }

// A segment number missing from the NZB itself (never indexed, no message ID)
// must deterministically defer the import for PAR2 repair — not fail archive
// analysis later with "incomplete NZB data, cannot auto-patch".
func TestPreParseFastFailDefersNzbGapInArchiveSet(t *testing.T) {
	client := fakepool.New() // every real segment STATs fine
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}
	n := gapTestNzb()

	_, _, _, err := proc.preParseFastFail(context.Background(), n, gapRepairConfig(), 1, nil, nil)

	var deferred *DeferredRepairError
	if !errors.As(err, &deferred) {
		t.Fatalf("err = %v, want DeferredRepairError", err)
	}
	wantID := nzbgap.SyntheticID("a2-1", 2)
	if deferred.FirstMissingSegmentID != wantID {
		t.Fatalf("FirstMissingSegmentID = %q, want synthetic %q", deferred.FirstMissingSegmentID, wantID)
	}
}

// Once the repair has stored a patch under the synthetic ID, the same NZB must
// pass fast-fail so the parked import can resume.
func TestPreParseFastFailPassesWhenGapIsPatched(t *testing.T) {
	client := fakepool.New()
	n := gapTestNzb()
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
		patchIndex:        fakePatchIndex{nzbgap.SyntheticID("a2-1", 2): true},
	}

	brokenIdx, _, _, err := proc.preParseFastFail(context.Background(), n, gapRepairConfig(), 1, nil, nil)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}
	if len(brokenIdx) != 0 {
		t.Fatalf("brokenIdx = %v, want none (gap is patched)", brokenIdx)
	}
	// The placeholder segment must now be part of the NZB so parsing and
	// archive coverage see a complete file.
	if got := len(n.Files[1].Segments); got != 3 {
		t.Fatalf("part02 segments = %d, want 3 (placeholder inserted)", got)
	}
}

// Without PAR2 files the gap cannot be repaired: the damaged set is excluded
// whole (both parts in brokenIdx), like any other broken archive set, while a
// healthy sibling set imports.
func TestPreParseFastFailGapWithoutPar2MarksSetBroken(t *testing.T) {
	client := fakepool.New()
	n := gapTestNzb()
	n.Files = n.Files[:2] // drop the PAR2 entry
	n.Files = append(n.Files, nzbparser.NzbFile{
		Filename: "setB.part01.rar",
		Subject:  "setB.part01.rar",
		Segments: nzbparser.NzbSegments{{Bytes: 100, Number: 1, ID: "b1-1"}},
	})
	proc := &Processor{
		poolManager:       processorTestPoolManager{client: client},
		validationTimeout: 100 * time.Millisecond,
	}

	brokenIdx, missingIDs, _, err := proc.preParseFastFail(context.Background(), n, gapRepairConfig(), 1, nil, nil)
	if err != nil {
		t.Fatalf("preParseFastFail returned error: %v", err)
	}
	if len(brokenIdx) != 2 {
		t.Fatalf("brokenIdx = %v, want both setA parts", brokenIdx)
	}
	for _, idx := range []int{0, 1} {
		if _, ok := brokenIdx[idx]; !ok {
			t.Errorf("brokenIdx missing index %d (setA part)", idx)
		}
	}
	if _, ok := missingIDs[nzbgap.SyntheticID("a2-1", 2)]; !ok {
		t.Fatalf("missingIDs = %v, want the synthetic gap id", missingIDs)
	}
}
