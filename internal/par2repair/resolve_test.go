package par2repair

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/javi11/nntppool/v4"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/par2gen"
)

// mkResolveFixture builds metadata + NzbStore + fake articles for a release
// of two content files with a full PAR2 set. Subjects optionally obfuscated.
func mkResolveFixture(t *testing.T, obfuscated bool) (*metapb.FileMetadata, *metapb.NzbStore, *fakeFetcher, map[string][]byte, string) {
	t.Helper()
	rng := rand.New(rand.NewSource(11))
	mk := func(n int) []byte {
		b := make([]byte, n)
		rng.Read(b)
		return b
	}
	contents := map[string][]byte{
		"a.rar": mk(8192),
		"b.rar": mk(8192),
	}
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{
		{Name: "a.rar", Content: contents["a.rar"]},
		{Name: "b.rar", Content: contents["b.rar"]},
	}, 6)

	fetch := &fakeFetcher{articles: map[string][]byte{}}
	store := &metapb.NzbStore{}
	const artSize = 2048

	// Content files: 4 articles each. Article 1 of a.rar is dead.
	deadID := "a.rar-1@test"
	for _, name := range []string{"a.rar", "b.rar"} {
		subject := fmt.Sprintf(`"%s" yEnc (1/4)`, name)
		if obfuscated {
			subject = "aGVsbG8gd29ybGQ" + name // no filename visible
		}
		entry := &metapb.NzbFileEntry{Subject: subject}
		content := contents[name]
		for off, i := 0, 0; off < len(content); off, i = off+artSize, i+1 {
			id := fmt.Sprintf("%s-%d@test", name, i)
			entry.Segments = append(entry.Segments, &metapb.NzbSeg{Id: id, Number: int32(i + 1), Bytes: artSize + 200})
			if id != deadID {
				fetch.articles[id] = content[off : off+artSize]
			}
		}
		store.Files = append(store.Files, entry)
	}

	// PAR2 files: index + volumes, one article each; recorded in metadata.
	fm := &metapb.FileMetadata{}
	par2Payloads := append([][]byte{set.Index}, set.Volumes...)
	for i, p := range par2Payloads {
		id := fmt.Sprintf("par2-%d@test", i)
		fetch.articles[id] = p
		store.Files = append(store.Files, &metapb.NzbFileEntry{
			Subject:  fmt.Sprintf(`"rel.vol%02d.par2" yEnc (1/1)`, i),
			Segments: []*metapb.NzbSeg{{Id: id, Number: 1, Bytes: int64(len(p))}},
		})
		fm.Par2Files = append(fm.Par2Files, &metapb.Par2FileReference{
			Filename: fmt.Sprintf("rel.vol%02d.par2", i),
			FileSize: int64(len(p)),
			SegmentData: []*metapb.SegmentData{
				{Id: "<" + id + ">", SegmentSize: int64(len(p))},
			},
		})
	}
	return fm, store, fetch, contents, deadID
}

func TestResolveByFilename(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)

	res, err := Resolve(context.Background(), fm, store, []string{"<" + deadID + ">"}, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Plan.Missing); got != 2 {
		t.Fatalf("Missing = %v", res.Plan.Missing)
	}
	if len(res.Plan.DeadArticles) != 1 || res.Plan.DeadArticles[0].MessageID != deadID {
		t.Fatalf("DeadArticles = %+v", res.Plan.DeadArticles)
	}
	// Article sizing: 4 articles of 2048 exactly.
	for _, f := range res.Plan.Files {
		if len(f.Articles) != 4 {
			t.Fatalf("articles = %d", len(f.Articles))
		}
		for _, a := range f.Articles {
			if a.Size != 2048 {
				t.Fatalf("article size = %d", a.Size)
			}
		}
	}
}

func TestResolveObfuscatedFallsBackToHash16k(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, true)

	res, err := Resolve(context.Background(), fm, store, []string{deadID}, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.DeadArticles) != 1 {
		t.Fatalf("DeadArticles = %+v", res.Plan.DeadArticles)
	}
}

func TestResolveEndToEndWithRunJob(t *testing.T) {
	fm, store, fetch, contents, deadID := mkResolveFixture(t, false)

	res, err := Resolve(context.Background(), fm, store, []string{deadID}, fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	ps := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), res.Plan, res.Index, res.Par2Files, fetch, ps, testLogger()); err != nil {
		t.Fatal(err)
	}
	got, ok := ps.Get(deadID)
	if !ok || !bytes.Equal(got, contents["a.rar"][2048:4096]) {
		t.Fatal("resolved repair did not reproduce the dead article payload")
	}
}

// mkVolumeFixture builds a release of two volumes with a full PAR2 set and no
// dead articles; the caller decides which message IDs to remove from the
// fetcher. Volume sizes differ so a part size borrowed from one file is
// genuinely validated against the other's length.
func mkVolumeFixture(t *testing.T) (*metapb.FileMetadata, *metapb.NzbStore, *fakeFetcher, map[string][]byte) {
	t.Helper()
	rng := rand.New(rand.NewSource(29))
	mk := func(n int) []byte {
		b := make([]byte, n)
		rng.Read(b)
		return b
	}
	contents := map[string][]byte{
		"vol1.rar": mk(4096),
		"vol2.rar": mk(8192),
	}
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{
		{Name: "vol1.rar", Content: contents["vol1.rar"]},
		{Name: "vol2.rar", Content: contents["vol2.rar"]},
	}, 8)

	fetch := &fakeFetcher{articles: map[string][]byte{}}
	store := &metapb.NzbStore{}
	const artSize = 2048
	for _, name := range []string{"vol1.rar", "vol2.rar"} {
		entry := &metapb.NzbFileEntry{Subject: fmt.Sprintf(`"%s" yEnc (1/4)`, name)}
		content := contents[name]
		for off, i := 0, 0; off < len(content); off, i = off+artSize, i+1 {
			id := fmt.Sprintf("%s-%d@test", name, i)
			entry.Segments = append(entry.Segments, &metapb.NzbSeg{Id: id, Number: int32(i + 1), Bytes: artSize + 200})
			fetch.articles[id] = content[off : off+artSize]
		}
		store.Files = append(store.Files, entry)
	}

	fm := &metapb.FileMetadata{}
	for i, p := range append([][]byte{set.Index}, set.Volumes...) {
		id := fmt.Sprintf("par2-%d@test", i)
		fetch.articles[id] = p
		store.Files = append(store.Files, &metapb.NzbFileEntry{
			Subject:  fmt.Sprintf(`"rel.vol%02d.par2" yEnc (1/1)`, i),
			Segments: []*metapb.NzbSeg{{Id: id, Number: 1, Bytes: int64(len(p))}},
		})
		fm.Par2Files = append(fm.Par2Files, &metapb.Par2FileReference{
			Filename:    fmt.Sprintf("rel.vol%02d.par2", i),
			FileSize:    int64(len(p)),
			SegmentData: []*metapb.SegmentData{{Id: "<" + id + ">", SegmentSize: int64(len(p))}},
		})
	}
	return fm, store, fetch, contents
}

// A volume whose articles are ALL missing is the canonical thing PAR2 repair
// exists to rebuild. Its decoded part size cannot be probed from its own
// articles, so it must be borrowed from a sibling volume of the same release.
func TestResolveFullyMissingVolume(t *testing.T) {
	fm, store, fetch, contents := mkVolumeFixture(t)

	dead := []string{"vol1.rar-0@test", "vol1.rar-1@test"}
	for _, id := range dead {
		delete(fetch.articles, id)
	}

	res, err := Resolve(context.Background(), fm, store, dead, fetch,
		Caps{MaxRepairRatio: 1.0, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatalf("fully missing volume must still be planned: %v", err)
	}
	if len(res.Plan.DeadArticles) != 2 {
		t.Fatalf("DeadArticles = %+v", res.Plan.DeadArticles)
	}
	for _, f := range res.Plan.Files {
		var sum int64
		for _, a := range f.Articles {
			if a.Size != 2048 {
				t.Fatalf("borrowed part size wrong: article size = %d", a.Size)
			}
			sum += a.Size
		}
		if sum != int64(f.Length) {
			t.Fatalf("article sizes sum to %d, want %d", sum, f.Length)
		}
	}

	// The layout must be right, not merely plausible: rebuild and compare.
	ps := NewPatchStore(t.TempDir())
	if err := RunJob(context.Background(), res.Plan, res.Index, res.Par2Files, fetch, ps, testLogger()); err != nil {
		t.Fatalf("RunJob: %v", err)
	}
	for i, id := range dead {
		got, ok := ps.Get(id)
		if !ok {
			t.Fatalf("no patch stored for %s", id)
		}
		want := contents["vol1.rar"][i*2048 : (i+1)*2048]
		if !bytes.Equal(got, want) {
			t.Fatalf("patch for %s does not match the original bytes", id)
		}
	}
}

// statFetcher wraps fakeFetcher with the ArticleStater capability, answering
// liveness from the articles map like a StatMany sweep would.
type statFetcher struct{ *fakeFetcher }

func (s statFetcher) StatIDs(_ context.Context, ids []string) (map[string]bool, error) {
	missing := map[string]bool{}
	for _, id := range ids {
		if _, ok := s.articles[id]; !ok {
			missing[id] = true
		}
	}
	return missing, nil
}

// A stat-capable fetcher lets Resolve discover dead articles that nobody
// declared, so the plan covers the release's full damage instead of tripping
// over surprises during the sweep.
func TestResolveStatSweepFindsUndeclaredDead(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)
	surprise := "b.rar-2@test"
	delete(fetch.articles, surprise)

	res, err := Resolve(context.Background(), fm, store, []string{deadID}, statFetcher{fetch},
		Caps{MaxRepairRatio: 1.0, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, da := range res.Plan.DeadArticles {
		got[da.MessageID] = true
	}
	if len(got) != 2 || !got[deadID] || !got[surprise] {
		t.Fatalf("DeadArticles = %+v, want %s and %s", res.Plan.DeadArticles, deadID, surprise)
	}
}

// When the stat sweep shows more damage than the recovery set can fix, the
// verdict must land at plan time — before any payload is downloaded.
func TestResolveUnrepairableFastWhenDamageExceedsRecovery(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)
	// 4 dead articles x 2 slices = 8 missing slices vs 6 recovery slices.
	// Only deadID is declared; the rest must come from the stat sweep.
	for _, id := range []string{"a.rar-2@test", "a.rar-3@test", "b.rar-2@test"} {
		delete(fetch.articles, id)
	}

	_, err := Resolve(context.Background(), fm, store, []string{deadID}, statFetcher{fetch},
		Caps{MaxRepairRatio: 1.0, MaxMemoryBytes: 64 << 20})
	if !errors.Is(err, ErrUnrepairable) {
		t.Fatalf("err = %v, want ErrUnrepairable", err)
	}
}

// A recovery slice whose payload tail lives in a dead article parses fine
// (ParseIndex seeks past payloads without fetching them), so the plan must
// drop it up front instead of discovering the hole mid-job.
func TestResolveDropsRecoverySlicesBackedByDeadArticles(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)

	// Re-split one PAR2 volume into two articles: a live head carrying the
	// packet header + exponent, and a dead tail carrying most of the payload.
	const volID = "par2-1@test"
	vol := fetch.articles[volID]
	const split = 128
	fetch.articles["par2-1a@test"] = vol[:split]
	delete(fetch.articles, volID) // tail article par2-1b is never added: dead
	for _, ref := range fm.Par2Files {
		if len(ref.SegmentData) == 1 && ref.SegmentData[0].Id == "<"+volID+">" {
			ref.SegmentData = []*metapb.SegmentData{
				{Id: "<par2-1a@test>", SegmentSize: split},
				{Id: "<par2-1b@test>", SegmentSize: int64(len(vol) - split)},
			}
		}
	}

	res, err := Resolve(context.Background(), fm, store, []string{deadID}, statFetcher{fetch},
		Caps{MaxRepairRatio: 1.0, MaxMemoryBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	if got := len(res.Plan.Recovery) + len(res.Plan.SpareRecovery); got != 5 {
		t.Fatalf("planned recovery slices = %d, want 5 (dead-backed slice dropped)", got)
	}
}

// noBodyFetcher is stat-capable but fails the test if any article body is
// actually downloaded.
type noBodyFetcher struct {
	t *testing.T
	statFetcher
}

func (f noBodyFetcher) Fetch(_ context.Context, messageID string) ([]byte, error) {
	f.t.Errorf("article body %s was fetched; the verdict must come from STATs alone", messageID)
	return nil, nntppool.ErrArticleNotFound
}

// Damage far above the repair-ratio cap must be rejected right after the
// liveness sweep — before the PAR2 set is parsed or any body is downloaded.
func TestResolveRatioCapRejectsEarlyWithoutBodyFetches(t *testing.T) {
	fm, store, fetch, _, deadID := mkResolveFixture(t, false)
	// Kill 6 of 8 content articles (75% of the release) against a 5% cap.
	for _, id := range []string{"a.rar-2@test", "a.rar-3@test", "b.rar-0@test", "b.rar-1@test", "b.rar-3@test"} {
		delete(fetch.articles, id)
	}

	_, err := Resolve(context.Background(), fm, store, []string{deadID},
		noBodyFetcher{t: t, statFetcher: statFetcher{fetch}},
		Caps{MaxRepairRatio: 0.05, MaxMemoryBytes: 64 << 20})
	if !errors.Is(err, ErrUnrepairable) {
		t.Fatalf("err = %v, want ErrUnrepairable", err)
	}
	if !strings.Contains(err.Error(), "max_repair_ratio") {
		t.Fatalf("err = %v, want a damage-ratio verdict", err)
	}
}

// The resolve-phase article cache must stay bounded: a 372-volume PAR2 set
// would otherwise pin hundreds of MB of header articles on the heap.
func TestArticleCacheEvictsOldestBeyondCap(t *testing.T) {
	c := newArticleCache(2)
	c.put("a", []byte("1"))
	c.put("b", []byte("2"))
	c.put("c", []byte("3"))
	if _, ok := c.get("a"); ok {
		t.Fatal("oldest entry must be evicted past the cap")
	}
	if _, ok := c.get("b"); !ok {
		t.Fatal("entry within cap must remain")
	}
	if got, ok := c.get("c"); !ok || string(got) != "3" {
		t.Fatal("newest entry must remain")
	}
}

func TestResolveNoPar2Files(t *testing.T) {
	_, store, fetch, _, _ := mkResolveFixture(t, false)
	fm := &metapb.FileMetadata{}
	_, err := Resolve(context.Background(), fm, store, nil, fetch, Caps{})
	if err == nil {
		t.Fatal("want error without par2 files")
	}
}
