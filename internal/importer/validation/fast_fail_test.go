package validation

import (
	"context"
	"fmt"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
)

type fastFailPoolManager struct {
	client *fakepool.Client
}

func (m fastFailPoolManager) GetPool() (pool.NntpClient, error) { return m.client, nil }
func (m fastFailPoolManager) HasPool() bool                     { return m.client != nil }
func (m fastFailPoolManager) IncArticlesDownloaded()            {}
func (m fastFailPoolManager) IncArticlesPosted()                {}
func (m fastFailPoolManager) UpdateDownloadProgress(string, int64) {
}
func (m fastFailPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}
func (m fastFailPoolManager) ResetMetrics(context.Context, bool, bool) error { return nil }
func (m fastFailPoolManager) ResetProviderErrors(context.Context) error      { return nil }
func (m fastFailPoolManager) SetProviders([]nntppool.Provider) error         { return nil }
func (m fastFailPoolManager) ClearPool() error                               { return nil }
func (m fastFailPoolManager) AddProvider(nntppool.Provider) error            { return nil }
func (m fastFailPoolManager) RemoveProvider(string) error                    { return nil }
func (m fastFailPoolManager) ResetProviderQuota(context.Context, string) error {
	return nil
}
func (m fastFailPoolManager) SetProviderIDs(map[string]string) {}
func (m fastFailPoolManager) AcquireImportSlot(context.Context) (func(), error) {
	return func() {}, nil
}
func (m fastFailPoolManager) SetAdmissionCaps(int, int)                 {}
func (m fastFailPoolManager) SetStreamSource(pool.StreamActivitySource) {}
func (m fastFailPoolManager) NotifyStreamChange()                       {}

func TestFastFailReleaseProbeUsesSegmentSamplePercentage(t *testing.T) {
	client := fakepool.New()
	files := []FastFailFile{
		{
			Filename: "movie.mkv",
			Segments: makeTestSegments("video", 100),
		},
	}

	missing, err := FastFailReleaseProbe(
		context.Background(),
		files,
		fastFailPoolManager{client: client},
		10,
		1,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("FastFailReleaseProbe returned error: %v", err)
	}
	if missing {
		t.Fatal("missing = true, want false (all segments reachable)")
	}

	if got := client.StatCalls(); got != 10 {
		t.Fatalf("StatCalls = %d, want 10 (10%% of 100 segments)", got)
	}
}

func TestFastFailReleaseProbeReportsMissingOnUnreachableSegment(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("rar-2", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{
			Filename: "release.part01.rar",
			Segments: []*metapb.SegmentData{
				{Id: "rar-0"},
				{Id: "rar-1"},
				{Id: "rar-2"},
			},
		},
	}

	missing, err := FastFailReleaseProbe(
		context.Background(),
		files,
		fastFailPoolManager{client: client},
		100,
		1,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("FastFailReleaseProbe error = %v, want nil (a missing segment is not an error)", err)
	}
	if !missing {
		t.Fatal("missing = false, want true (rar-2 is unreachable)")
	}
}

func TestFastFailReleaseProbePoolUnavailableReturnsError(t *testing.T) {
	missing, err := FastFailReleaseProbe(
		context.Background(),
		[]FastFailFile{{Filename: "movie.mkv", Segments: makeTestSegments("v", 5)}},
		fastFailPoolManager{client: nil},
		100,
		1,
		100*time.Millisecond,
	)
	if err == nil {
		t.Fatal("FastFailReleaseProbe returned nil error, want error for nil pool")
	}
	if missing {
		t.Error("missing = true, want false when the pool is unavailable (infra error path)")
	}
}

func TestFastFailReleaseProbeNoSegmentsIsHealthy(t *testing.T) {
	client := fakepool.New()
	missing, err := FastFailReleaseProbe(
		context.Background(),
		[]FastFailFile{{Filename: "release.par2"}}, // PAR2 slot: no segments
		fastFailPoolManager{client: client},
		100,
		1,
		100*time.Millisecond,
	)
	if err != nil {
		t.Fatalf("FastFailReleaseProbe error = %v, want nil", err)
	}
	if missing {
		t.Error("missing = true, want false when there are no segments to probe")
	}
	if got := client.StatCalls(); got != 0 {
		t.Errorf("StatCalls = %d, want 0 (nothing to probe)", got)
	}
}

func TestSelectFastFailSegments(t *testing.T) {
	idOf := func(segs []*metapb.SegmentData) map[string]struct{} {
		m := make(map[string]struct{}, len(segs))
		for _, s := range segs {
			m[s.Id] = struct{}{}
		}
		return m
	}

	// len <= 2 returns all segments unchanged.
	two := makeTestSegments("s", 2)
	if got := selectFastFailSegments(two, 100); len(got) != 2 {
		t.Fatalf("len<=2: got %d segments, want 2", len(got))
	}

	segs := makeTestSegments("s", 100)

	// pct=0 → exactly first + last.
	got := selectFastFailSegments(segs, 0)
	if len(got) != 2 {
		t.Fatalf("pct=0: got %d, want 2 (first+last)", len(got))
	}
	if got[0].Id != segs[0].Id || got[1].Id != segs[len(segs)-1].Id {
		t.Fatalf("pct=0: want first=%s last=%s, got %s,%s", segs[0].Id, segs[99].Id, got[0].Id, got[1].Id)
	}

	// pct=10 of 100 → 10 middle + first + last = 12, with first/last present and no dups.
	got = selectFastFailSegments(segs, 10)
	if len(got) != 12 {
		t.Fatalf("pct=10: got %d, want 12", len(got))
	}
	ids := idOf(got)
	if _, ok := ids[segs[0].Id]; !ok {
		t.Error("pct=10: first segment missing")
	}
	if _, ok := ids[segs[99].Id]; !ok {
		t.Error("pct=10: last segment missing")
	}
	if len(ids) != len(got) {
		t.Errorf("pct=10: duplicates present (%d unique of %d)", len(ids), len(got))
	}

	// Total is capped at 55 even at 100% sampling, with no dups.
	big := selectFastFailSegments(makeTestSegments("b", 10000), 100)
	if len(big) != 55 {
		t.Fatalf("cap: got %d, want 55", len(big))
	}
	if len(idOf(big)) != 55 {
		t.Error("cap: duplicates present in capped result")
	}
}

func makeTestSegments(prefix string, count int) []*metapb.SegmentData {
	segments := make([]*metapb.SegmentData, count)
	for i := range count {
		segments[i] = &metapb.SegmentData{Id: fmt.Sprintf("%s-%d", prefix, i)}
	}
	return segments
}

// FastFailCheckFiles tests

func TestFastFailCheckFilesAllReachable(t *testing.T) {
	client := fakepool.New()
	files := []FastFailFile{
		{Filename: "movie.mkv", Segments: makeTestSegments("video", 5)},
		{Filename: "extras.mkv", Segments: makeTestSegments("extras", 5)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 2, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for i, r := range results {
		if r.Broken {
			t.Errorf("results[%d].Broken = true, want false", i)
		}
	}
}

func TestFastFailCheckFilesOneFileBroken(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("bad-0", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	client.SetBehavior("bad-1", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
	client.SetBehavior("bad-2", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "good.mkv", Segments: makeTestSegments("good", 3)},
		{Filename: "broken.mkv", Segments: makeTestSegments("bad", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 2, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	if results[0].Broken {
		t.Errorf("results[0].Broken = true, want false (good file)")
	}
	if !results[1].Broken {
		t.Errorf("results[1].Broken = false, want true (broken file)")
	}
	if len(results[1].MissingSegmentIDs) == 0 {
		t.Errorf("results[1].MissingSegmentIDs is empty, want populated")
	}
}

func TestFastFailCheckFilesBrokenSidecarsAreReported(t *testing.T) {
	client := fakepool.New()
	client.SetDefaultBehavior(fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "readme.nfo", Segments: makeTestSegments("nfo", 3)},
		{Filename: "checksum.sfv", Segments: makeTestSegments("sfv", 3)},
		{Filename: "cover.jpg", Segments: makeTestSegments("jpg", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 2, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	for i, r := range results {
		if !r.Broken {
			t.Errorf("results[%d].Broken = false for sidecar %q, want true (missing segments)", i, files[i].Filename)
		}
	}
	if client.StatCalls() == 0 {
		t.Errorf("StatCalls = 0, want >0 (all files must be checked)")
	}
}

// TestFastFailCheckFilesBrokenSidecarIsReported verifies that a sidecar file
// (.nfo, .sfv, etc.) with missing segments IS reported broken — all files are
// now checked regardless of extension, so broken sidecars are excluded from
// parsing just like broken media files.
func TestFastFailCheckFilesBrokenSidecarIsReported(t *testing.T) {
	client := fakepool.New()
	client.SetDefaultBehavior(fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "readme.nfo", Segments: makeTestSegments("nfo", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 1, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Broken {
		t.Error("results[0].Broken = false, want true: sidecar with missing segments must be reported broken")
	}
	if client.StatCalls() == 0 {
		t.Error("StatCalls = 0, want >0: sidecar must be Stat-checked")
	}
}

func TestFastFailCheckFilesPoolUnavailableReturnsError(t *testing.T) {
	_, err := FastFailCheckFiles(
		context.Background(),
		[]FastFailFile{{Filename: "movie.mkv", Segments: makeTestSegments("v", 1)}},
		fastFailPoolManager{client: nil},
		100, 1, 100*time.Millisecond,
		nil,
	)
	if err == nil {
		t.Fatal("FastFailCheckFiles returned nil error, want error for nil pool")
	}
}

// TestFastFailCheckFilesFirstSegmentAlwaysChecked verifies that Segments[0] is
// always Stat-checked even when sampling would otherwise omit it.
// We pass samplePercentage=0, which forces SelectSegmentsForValidation to its
// minimum of 5 segments. For a 1-segment file the minimum is 1, so this already
// exercises the guarantee. For a large file we confirm segment-0 is checked by
// making only segment-0 unreachable — the file must be reported broken.
func TestFastFailCheckFilesFirstSegmentAlwaysChecked(t *testing.T) {
	client := fakepool.New()
	// Only segment-0 is missing. All others are reachable.
	client.SetBehavior("only-0", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	// Build a file where "only-0" is index 0 and the rest are reachable.
	segs := []*metapb.SegmentData{{Id: "only-0"}}
	for i := 1; i <= 4; i++ {
		segs = append(segs, &metapb.SegmentData{Id: fmt.Sprintf("only-%d", i)})
	}

	files := []FastFailFile{
		{Filename: "movie.mkv", Segments: segs},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		0, 1, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Broken {
		t.Error("results[0].Broken = false, want true: first segment is missing but was not checked")
	}
}

// TestFastFailCheckFilesGroupPropagation verifies that when one part of a RAR
// set is unreachable, every member of that set is marked Broken, while an
// ungrouped sibling file stays healthy.
func TestFastFailCheckFilesGroupPropagation(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("setA-1-0", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "setA.part01.rar", Segments: makeTestSegments("setA-0", 3), GroupKey: "seta"},
		{Filename: "setA.part02.rar", Segments: makeTestSegments("setA-1", 3), GroupKey: "seta"},
		{Filename: "setA.part03.rar", Segments: makeTestSegments("setA-2", 3), GroupKey: "seta"},
		{Filename: "standalone.mkv", Segments: makeTestSegments("solo", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 4, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	for i := range 3 {
		if !results[i].Broken {
			t.Errorf("results[%d].Broken = false, want true (whole set is doomed)", i)
		}
	}
	if results[3].Broken {
		t.Error("results[3].Broken = true, want false (standalone file is healthy)")
	}
	// Only the observed-missing segment is reported; siblings carry none.
	if len(results[0].MissingSegmentIDs) != 0 {
		t.Errorf("results[0].MissingSegmentIDs = %v, want empty (no observed miss)", results[0].MissingSegmentIDs)
	}
}

// TestFastFailCheckFilesGroupShortCircuitSkipsStats verifies that once a group
// is known broken, the remaining Stats for that group are skipped — fewer Stats
// run than the total selected sample count.
func TestFastFailCheckFilesGroupShortCircuitSkipsStats(t *testing.T) {
	client := fakepool.New()
	// Every segment of the broken set is unreachable, so any Stat that runs fails;
	// the point is that most are skipped after the first miss.
	client.SetDefaultBehavior(fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	const parts = 20
	const segsPerPart = 3
	files := make([]FastFailFile, parts)
	for i := range files {
		files[i] = FastFailFile{
			Filename: fmt.Sprintf("doomed.part%02d.rar", i+1),
			Segments: makeTestSegments(fmt.Sprintf("p%d", i), segsPerPart),
			GroupKey: "doomed",
		}
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 1, 100*time.Millisecond, // single connection → deterministic ordering
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	for i, r := range results {
		if !r.Broken {
			t.Errorf("results[%d].Broken = false, want true", i)
		}
	}
	// Round-robin means the first round Stats one segment per part (20 Stats),
	// the very first of which marks the group broken; all later Stats are skipped.
	// So total Stats must be well below the full sample of parts*segsPerPart.
	if got, full := client.StatCalls(), int64(parts*segsPerPart); got >= full {
		t.Errorf("StatCalls = %d, want < %d (group short-circuit must skip Stats)", got, full)
	}
}

// TestFastFailCheckFilesEmptyGroupKeyNoPropagation guards against propagation
// leaking across ungrouped files: two standalone files, one broken, must not
// taint the other.
func TestFastFailCheckFilesEmptyGroupKeyNoPropagation(t *testing.T) {
	client := fakepool.New()
	client.SetBehavior("a-0", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "a.mkv", Segments: makeTestSegments("a", 3)},
		{Filename: "b.mkv", Segments: makeTestSegments("b", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 2, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v, want nil", err)
	}
	if !results[0].Broken {
		t.Error("results[0].Broken = false, want true")
	}
	if results[1].Broken {
		t.Error("results[1].Broken = true, want false (empty GroupKey must not propagate)")
	}
}

func TestFastFailCheckFilesIndexAligned(t *testing.T) {
	client := fakepool.New()
	// Only the middle file's segments fail.
	client.SetBehavior("mid-0", fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})

	files := []FastFailFile{
		{Filename: "first.mkv", Segments: makeTestSegments("first", 3)},
		{Filename: "second.mkv", Segments: []*metapb.SegmentData{{Id: "mid-0"}}},
		{Filename: "third.mkv", Segments: makeTestSegments("third", 3)},
	}

	results, err := FastFailCheckFiles(
		context.Background(), files,
		fastFailPoolManager{client: client},
		100, 2, 100*time.Millisecond,
		nil,
	)
	if err != nil {
		t.Fatalf("FastFailCheckFiles error = %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	if results[0].Broken {
		t.Error("results[0].Broken = true, want false")
	}
	if !results[1].Broken {
		t.Error("results[1].Broken = false, want true")
	}
	if results[2].Broken {
		t.Error("results[2].Broken = true, want false")
	}
}
