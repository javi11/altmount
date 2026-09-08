package validation

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
)

// A dead post used to be STAT-ed 64 times, three attempts over, then swept per
// file: hundreds of slow 430 lookups left pipelined on the connections, which
// the next import's STATs then queued behind. Eight sampled articles all
// missing is already the verdict.
func TestFastFailReleaseProbeVerdictJudgesDeadPostFromFirstWave(t *testing.T) {
	outcomes := make(map[string][]error, 64)
	for _, seg := range makeTestSegments("seg", 64) {
		outcomes[seg.Id] = []error{nntppool.ErrArticleNotFound}
	}
	client := newScriptedStatClient(outcomes)

	v, err := FastFailReleaseProbeVerdict(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err != nil {
		t.Fatalf("FastFailReleaseProbeVerdict error = %v", err)
	}
	if !v.Missing || !v.Dead {
		t.Fatalf("verdict = %+v, want Missing and Dead", v)
	}
	total := 0
	for _, seg := range makeTestSegments("seg", 64) {
		total += client.callCount(seg.Id)
	}
	if total > probeFirstWave {
		t.Fatalf("STATs issued = %d, want at most the first wave of %d", total, probeFirstWave)
	}
	if len(v.MissingIDs) == 0 {
		t.Fatal("verdict carries no missing ids for the caller to record")
	}
}

func TestFastFailReleaseProbeVerdictHealthyPostChecksWholeSample(t *testing.T) {
	client := newScriptedStatClient(nil)

	v, err := FastFailReleaseProbeVerdict(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err != nil || v.Missing || v.Dead {
		t.Fatalf("verdict = %+v, err = %v, want healthy", v, err)
	}
	total := 0
	for _, seg := range makeTestSegments("seg", 64) {
		total += client.callCount(seg.Id)
	}
	if total != 64 {
		t.Fatalf("STATs issued = %d, want the whole 64-article sample on a healthy post", total)
	}
}

func TestFastFailReleaseProbeVerdictPartialDamageIsNotDead(t *testing.T) {
	client := newScriptedStatClient(map[string][]error{"seg-1": {nntppool.ErrArticleNotFound}})

	v, err := FastFailReleaseProbeVerdict(context.Background(), probeFile(64), fastFailPoolManager{client: client}, 100, 64, 30*time.Second, nil)
	if err != nil {
		t.Fatalf("FastFailReleaseProbeVerdict error = %v", err)
	}
	if !v.Missing || v.Dead {
		t.Fatalf("verdict = %+v, want Missing but not Dead: one miss of eight is damage to map, not a dead post", v)
	}
}

func TestDeadReleaseResultsMarkEveryFileBroken(t *testing.T) {
	files := []FastFailFile{
		{Filename: "a.part01.rar", Segments: makeTestSegments("a", 3), GroupKey: "a"},
		{Filename: "a.part02.rar", Segments: makeTestSegments("b", 3), GroupKey: "a"},
		{Filename: "a.par2"},
	}
	results := DeadReleaseResults(files, []string{"a-0", "b-2"})
	if len(results) != 3 {
		t.Fatalf("results = %d, want one per file", len(results))
	}
	if !results[0].Broken || !results[1].Broken {
		t.Fatalf("files with segments not marked broken: %+v", results[:2])
	}
	if results[2].Broken {
		t.Fatal("segment-less sidecar marked broken")
	}
	if got := results[0].MissingSegmentIDs; len(got) != 1 || got[0] != "a-0" {
		t.Fatalf("file 0 missing ids = %v, want [a-0]", got)
	}
	if got := results[1].MissingSegmentIDs; len(got) != 1 || got[0] != "b-2" {
		t.Fatalf("file 1 missing ids = %v, want [b-2]", got)
	}
}
