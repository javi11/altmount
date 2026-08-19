package par2repair

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	"github.com/javi11/altmount/internal/testsupport/par2gen"
)

// fakeFetcher serves article payloads from a map; absent keys are dead.
// Concurrency-safe like the production fetcher must be.
type fakeFetcher struct {
	mu       sync.Mutex
	articles map[string][]byte
	fetched  []string
}

func (f *fakeFetcher) Fetch(_ context.Context, messageID string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	data, ok := f.articles[messageID]
	if !ok {
		return nil, nntppool.ErrArticleNotFound
	}
	f.fetched = append(f.fetched, messageID)
	return data, nil
}

// repairFixture wires a complete fake release: two content files split into
// articles, a full PAR2 set split into one article per par2 file, and the
// SetFile/article maps for both.
type repairFixture struct {
	idx        *par2.Index
	files      []SetFile
	par2Files  []SetFile
	fetch      *fakeFetcher
	contents   map[string][]byte // by name
	deadMsgID  string
	deadOrig   []byte // the dead article's true payload
	deadOffset int64
}

func mkRepairFixture(t *testing.T, sliceSize int, artSize int64, numRecovery int, deadArt int) *repairFixture {
	t.Helper()
	rng := rand.New(rand.NewSource(7))
	mk := func(n int) []byte {
		b := make([]byte, n)
		rng.Read(b)
		return b
	}
	contents := map[string][]byte{
		"a.rar": mk(8192),
		"b.rar": mk(8192),
	}
	set := par2gen.BuildFull(sliceSize, []par2gen.FileEntry{
		{Name: "a.rar", Content: contents["a.rar"]},
		{Name: "b.rar", Content: contents["b.rar"]},
	}, numRecovery)

	streams := []io.Reader{bytes.NewReader(set.Index)}
	for _, v := range set.Volumes {
		streams = append(streams, bytes.NewReader(v))
	}
	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatal(err)
	}

	fetch := &fakeFetcher{articles: map[string][]byte{}}
	fx := &repairFixture{idx: idx, fetch: fetch, contents: contents}

	// Content files -> articles. Article deadArt of a.rar is dead.
	for _, name := range []string{"a.rar", "b.rar"} {
		content := contents[name]
		var fileID [16]byte
		for id, fd := range idx.Files {
			if fd.Name == name {
				fileID = id
			}
		}
		sf := SetFile{FileID: fileID, Length: uint64(len(content))}
		for off, i := int64(0), 0; off < int64(len(content)); off, i = off+artSize, i+1 {
			sz := min(artSize, int64(len(content))-off)
			msgID := fmt.Sprintf("<%s-%d@test>", name, i)
			payload := content[off : off+sz]
			dead := name == "a.rar" && i == deadArt
			if dead {
				fx.deadMsgID = msgID
				fx.deadOrig = payload
				fx.deadOffset = off
			} else {
				fetch.articles[msgID] = payload
			}
			sf.Articles = append(sf.Articles, Article{MessageID: msgID, Size: sz, Dead: dead})
		}
		fx.files = append(fx.files, sf)
	}

	// PAR2 files -> one article each, matching ParseIndex stream order.
	par2Payloads := append([][]byte{set.Index}, set.Volumes...)
	for i, p := range par2Payloads {
		msgID := fmt.Sprintf("<par2-%d@test>", i)
		fetch.articles[msgID] = p
		fx.par2Files = append(fx.par2Files, SetFile{
			Length:   uint64(len(p)),
			Articles: []Article{{MessageID: msgID, Size: int64(len(p))}},
		})
	}
	return fx
}

func testLogger() *slog.Logger { return slog.New(slog.DiscardHandler) }

func TestRunJobRepairsDeadArticle(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}

	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get(fx.deadMsgID)
	if !ok {
		t.Fatal("no patch stored for dead article")
	}
	if !bytes.Equal(got, fx.deadOrig) {
		t.Fatalf("patch mismatch: got %d bytes, want %d byte-exact", len(got), len(fx.deadOrig))
	}
}

// concurrencyFetcher records the peak number of Fetch calls in flight.
type concurrencyFetcher struct {
	inner *fakeFetcher

	mu          sync.Mutex
	inFlight    int
	maxInFlight int
}

func (c *concurrencyFetcher) Fetch(ctx context.Context, messageID string) ([]byte, error) {
	c.mu.Lock()
	c.inFlight++
	c.maxInFlight = max(c.maxInFlight, c.inFlight)
	c.mu.Unlock()
	time.Sleep(time.Millisecond) // give overlapping fetches a chance to pile up
	defer func() {
		c.mu.Lock()
		c.inFlight--
		c.mu.Unlock()
	}()
	return c.inner.Fetch(ctx, messageID)
}

// The sweep of a 900MB release must not download one article at a time: RunJob
// is expected to keep several fetches in flight while folding slices in order.
func TestRunJobFetchesArticlesConcurrently(t *testing.T) {
	// Small articles (512B) over 2x8192B files -> 32 sweep articles.
	fx := mkRepairFixture(t, 1024, 512, 6, 1)
	fetch := &concurrencyFetcher{inner: fx.fetch}

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("parallel sweep must still produce a byte-exact patch")
	}
	if fetch.maxInFlight < 2 {
		t.Fatalf("max in-flight fetches = %d, want concurrent fetching", fetch.maxInFlight)
	}
}

// A plan over the memory budget must still repair, backed by disk instead of
// heap accumulators. maxInFlight and byte-exactness must both hold.
func TestRunJobSpillToDiskRepairs(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 1}) // force spill
	if err != nil {
		t.Fatal(err)
	}
	if !plan.SpillToDisk {
		t.Fatal("fixture must produce a spill plan")
	}

	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("disk-backed repair must produce a byte-exact patch")
	}
}

// Spill mode must absorb a corrupt present slice on its margin rows, all on
// disk-backed buffers.
func TestRunJobSpillToDiskSurvivesCorruptReplan(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	corruptID := "<b.rar-0@test>"
	corrupted := bytes.Clone(fx.fetch.articles[corruptID])
	corrupted[10] ^= 0xFF
	fx.fetch.articles[corruptID] = corrupted

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 1})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("disk-backed replan must produce a byte-exact patch")
	}
}

func TestRunJobRepairsDespiteCorruptPresentSlice(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	// Corrupt a present content article (same length, one flipped byte): the
	// swept slice fails its IFSC CRC32 and must be reclassified as missing,
	// consuming a spare recovery slice.
	corruptID := "<b.rar-0@test>"
	corrupted := bytes.Clone(fx.fetch.articles[corruptID])
	corrupted[10] ^= 0xFF
	fx.fetch.articles[corruptID] = corrupted

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}

	got, ok := store.Get(fx.deadMsgID)
	if !ok {
		t.Fatal("no patch stored for dead article")
	}
	if !bytes.Equal(got, fx.deadOrig) {
		t.Fatalf("patch mismatch: got %d bytes, want %d byte-exact", len(got), len(fx.deadOrig))
	}
	// The corrupt article is still served by providers; no patch for it.
	if store.Has(corruptID) {
		t.Fatal("must not store a patch for a present (corrupt) article")
	}
}

func TestRunJobUnrepairableWhenCorruptAndNoSpares(t *testing.T) {
	// numRecovery=2 exactly covers the dead article's 2 missing slices
	// (slice size 1024, article size 2048), leaving zero spares.
	fx := mkRepairFixture(t, 1024, 2048, 2, 1)
	corruptID := "<b.rar-0@test>"
	corrupted := bytes.Clone(fx.fetch.articles[corruptID])
	corrupted[10] ^= 0xFF
	fx.fetch.articles[corruptID] = corrupted

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	err = RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger())
	if !errors.Is(err, ErrUnrepairable) {
		t.Fatalf("err = %v, want ErrUnrepairable", err)
	}
	if store.Has(fx.deadMsgID) {
		t.Fatal("patch must not be stored after an aborted job")
	}
}

func TestRunJobFallsBackToSpareOnDeadRecoveryArticle(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	// Kill the par2 volume article that carries the first chosen recovery slice.
	firstRef := plan.Recovery[0]
	deadPar2 := fx.par2Files[firstRef.FileIndex].Articles[0].MessageID
	delete(fx.fetch.articles, deadPar2)

	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("repair via spare recovery slice failed")
	}
}

func TestRunJobUnrepairableWhenAllRecoveryDead(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 2, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	// Kill every par2 volume (streams 1..N; stream 0 is the index file).
	for i := 1; i < len(fx.par2Files); i++ {
		delete(fx.fetch.articles, fx.par2Files[i].Articles[0].MessageID)
	}
	store := NewPatchStore(t.TempDir())
	err = RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger())
	if !errors.Is(err, ErrUnrepairable) {
		t.Fatalf("err = %v, want ErrUnrepairable", err)
	}
}

// countFetches returns how many times messageID was successfully fetched.
func (f *fakeFetcher) countFetches(messageID string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, id := range f.fetched {
		if id == messageID {
			n++
		}
	}
	return n
}

// An article the plan thought live but that dies before the sweep reaches it
// must be absorbed by the plan's margin recovery rows: the job completes in
// one sweep and patches the newly dead article too.
func TestRunJobAbsorbsMidSweepDeadArticle(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Recovery) <= len(plan.Missing) {
		t.Fatal("fixture must produce a plan with margin rows")
	}

	// Kill a b.rar article after planning: the plan still marks it live.
	lateDead := "<b.rar-1@test>"
	lateOrig := fx.fetch.articles[lateDead]
	delete(fx.fetch.articles, lateDead)

	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatalf("margin rows must absorb a mid-sweep dead article, got %v", err)
	}

	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("planned dead article not repaired byte-exact")
	}
	got, ok = store.Get(lateDead)
	if !ok || !bytes.Equal(got, lateOrig) {
		t.Fatal("mid-sweep dead article not repaired byte-exact")
	}
	// One sweep only: no present article was read twice.
	if n := fx.fetch.countFetches("<a.rar-0@test>"); n != 1 {
		t.Fatalf("article <a.rar-0@test> fetched %d times, want 1 (single sweep)", n)
	}
}

// A mid-sweep dead article with no margin rows left must still surface
// SweepDeadArticleError so the service can persist the discovery and replan.
func TestRunJobMidSweepDeadWithoutMarginSurfacesError(t *testing.T) {
	// numRecovery=2 exactly covers the dead article's 2 missing slices:
	// no margin, no spares.
	fx := mkRepairFixture(t, 1024, 2048, 2, 1)
	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	delete(fx.fetch.articles, "<b.rar-1@test>")

	store := NewPatchStore(t.TempDir())
	err = RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger())
	var dead *SweepDeadArticleError
	if !errors.As(err, &dead) || dead.MessageID != "<b.rar-1@test>" {
		t.Fatalf("err = %v, want SweepDeadArticleError for <b.rar-1@test>", err)
	}
}

// A corrupt present slice must be absorbed by a margin row in the same sweep,
// not trigger a second full read of the release.
func TestRunJobAbsorbsCorruptSliceWithoutResweep(t *testing.T) {
	fx := mkRepairFixture(t, 1024, 2048, 6, 1)
	corruptID := "<b.rar-0@test>"
	corrupted := bytes.Clone(fx.fetch.articles[corruptID])
	corrupted[10] ^= 0xFF
	fx.fetch.articles[corruptID] = corrupted

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("repair with absorbed corrupt slice not byte-exact")
	}
	if n := fx.fetch.countFetches("<a.rar-0@test>"); n != 1 {
		t.Fatalf("article <a.rar-0@test> fetched %d times, want 1 (corrupt slice must not force a re-sweep)", n)
	}
}

// More corrupt slices than margin rows must fall back to the replan path:
// one spare per overflow slice and a fresh sweep, whose recovery payloads are
// refetched (the previous attempt's fold consumed the donated buffers).
func TestRunJobCorruptOverflowFallsBackToReplan(t *testing.T) {
	// sliceSize == artSize == 512: one slice per article. The dead a.rar
	// article is 1 missing slice, so the plan carries 1+8 recovery rows;
	// 12 volumes leave 3 spares.
	fx := mkRepairFixture(t, 512, 512, 12, 1)
	// Corrupt 10 b.rar articles: 8 absorbed on margin rows, 2 via replans.
	for i := range 10 {
		id := fmt.Sprintf("<b.rar-%d@test>", i)
		corrupted := bytes.Clone(fx.fetch.articles[id])
		corrupted[3] ^= 0xFF
		fx.fetch.articles[id] = corrupted
	}

	plan, err := BuildPlan(fx.idx, fx.files, Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Recovery) != 9 || len(plan.SpareRecovery) != 3 {
		t.Fatalf("recovery split = %d/%d, want 9/3", len(plan.Recovery), len(plan.SpareRecovery))
	}
	store := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), plan, fx.idx, fx.par2Files, fx.fetch, store, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := store.Get(fx.deadMsgID)
	if !ok || !bytes.Equal(got, fx.deadOrig) {
		t.Fatal("repair across corrupt-overflow replans not byte-exact")
	}
	// The replans really happened: present articles were swept more than once.
	if n := fx.fetch.countFetches("<a.rar-0@test>"); n < 2 {
		t.Fatalf("article <a.rar-0@test> fetched %d times, want >=2 (replan re-sweeps)", n)
	}
}
