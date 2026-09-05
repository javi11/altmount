package metadata

import (
	"testing"
	"time"

	"github.com/javi11/altmount/internal/holes"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

func TestLiveKnownHolesTrustsOnlyFreshRunsUnderTheSameProviders(t *testing.T) {
	now := time.Now()
	rows := []*metapb.HoleRun{
		{StartSegment: 1, Count: 2, RecordedAt: now.Add(-time.Hour).Unix()},
		{StartSegment: 10, Count: 1, RecordedAt: now.Add(-25 * time.Hour).Unix()},
		{StartSegment: 20, Count: 1}, // legacy, unstamped
	}
	live := LiveKnownHoles(rows, "fp-a", "fp-a", now)
	if len(live) != 1 || live[0].Start != 1 || live[0].Count != 2 {
		t.Fatalf("live = %+v, want only the hour-old run", live)
	}
	if got := LiveKnownHoles(rows, "fp-a", "fp-b", now); len(got) != 0 {
		t.Fatalf("foreign fingerprint must trust nothing, got %+v", got)
	}
	if got := LiveKnownHoles(rows, "", "fp-a", now); len(got) != 0 {
		t.Fatalf("legacy records without a fingerprint must trust nothing, got %+v", got)
	}
}

func TestMergeStampedKeepsOldStampsAndStampsNewRuns(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * time.Hour).Unix()
	stored := []*metapb.HoleRun{
		{StartSegment: 1, Count: 2, RecordedAt: old},
		{StartSegment: 50, Count: 1, RecordedAt: old},
	}
	out := mergeStamped(stored, []holes.Run{{Start: 3, Count: 1}, {Start: 80, Count: 1}}, now)
	byStart := map[int64]*metapb.HoleRun{}
	for _, r := range out {
		byStart[r.StartSegment] = r
	}
	if r := byStart[1]; r == nil || r.Count != 3 || r.RecordedAt != now.Unix() {
		t.Fatalf("merged run touching a new miss must be re-stamped: %+v", r)
	}
	if r := byStart[50]; r == nil || r.RecordedAt != old {
		t.Fatalf("untouched run must keep its stamp: %+v", r)
	}
	if r := byStart[80]; r == nil || r.RecordedAt != now.Unix() {
		t.Fatalf("new run must be stamped now: %+v", r)
	}
}

func TestAddKnownHolesDropsRunsFromAnotherProviderSet(t *testing.T) {
	ms := NewMetadataService(t.TempDir())
	path := "movies/x.mkv"
	if err := ms.WriteFileMetadata(path, &metapb.FileMetadata{FileSize: 10, Status: metapb.FileStatus_FILE_STATUS_HEALTHY}); err != nil {
		t.Fatal(err)
	}
	if err := ms.AddKnownHoles(path, []holes.Run{{Start: 1, Count: 1}}, "fp-a"); err != nil {
		t.Fatal(err)
	}
	if err := ms.AddKnownHoles(path, []holes.Run{{Start: 7, Count: 1}}, "fp-b"); err != nil {
		t.Fatal(err)
	}
	m, err := ms.ReadFileMetadata(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.HoleProviderFingerprint != "fp-b" || len(m.KnownHoles) != 1 || m.KnownHoles[0].StartSegment != 7 {
		t.Fatalf("a new provider set must replace old holes: fp=%q holes=%+v", m.HoleProviderFingerprint, m.KnownHoles)
	}
	if m.KnownHoles[0].RecordedAt == 0 {
		t.Fatal("stored run must be stamped")
	}
}
