package par2repair

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// countingFetcher wraps fakeFetcher counting Fetch calls per id.
type countingFetcher struct {
	mu    sync.Mutex
	inner *fakeFetcher
	calls map[string]int
}

func (c *countingFetcher) Fetch(ctx context.Context, id string) ([]byte, error) {
	c.mu.Lock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[id]++
	c.mu.Unlock()
	return c.inner.Fetch(ctx, id)
}

func (c *countingFetcher) total() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	n := 0
	for _, v := range c.calls {
		n += v
	}
	return n
}

// A PAR2 volume whose articles die one by one on Body (STAT said alive, Body
// 430s) must not be probed article-by-article to the end: with a flapping
// provider each probe costs tens of seconds, so an unbounded walk turns
// planning into hours of silence. After a bounded number of failed probes the
// volume defers to the release-wide part size.
func TestSizePar2SetFilesCapsProbesPerVolume(t *testing.T) {
	const decodedPart = 700_000
	mkVol := func(name string, n int) SetFile {
		sf := SetFile{}
		for j := range n {
			sf.Articles = append(sf.Articles, Article{
				MessageID: fmt.Sprintf("%s-%d@test", name, j),
				Size:      721_000,
			})
			sf.Length += 721_000
		}
		return sf
	}
	// Volume 0 probeable; volume 1 has 40 articles that all 430 on Body.
	files := []SetFile{mkVol("v0", 3), mkVol("v1", 40)}
	inner := &fakeFetcher{articles: map[string][]byte{}}
	for j := range 3 {
		inner.articles[fmt.Sprintf("v0-%d@test", j)] = make([]byte, decodedPart)
	}
	fetch := &countingFetcher{inner: inner}

	dead := map[string]bool{}
	if err := sizePar2SetFiles(context.Background(), fetch, files, dead,
		newArticleCache(64), testLogger()); err != nil {
		t.Fatal(err)
	}

	if files[1].SizeSource != SizeBorrowedHint {
		t.Fatalf("volume 1 size source = %v, want borrowed_hint", files[1].SizeSource)
	}
	if files[1].Articles[0].Size != decodedPart {
		t.Fatalf("volume 1 part size = %d, want borrowed %d", files[1].Articles[0].Size, decodedPart)
	}
	// The 40-article volume must not have been probed article by article:
	// bounded probes (cap + the final-article probe + the two first/last
	// prewarms, which stay uncached when the fetch fails), not ~40.
	v1Fetches := 0
	fetch.mu.Lock()
	for id, n := range fetch.calls {
		if len(id) > 2 && id[:2] == "v1" {
			v1Fetches += n
		}
	}
	fetch.mu.Unlock()
	if v1Fetches > maxSizeProbesPerFile+3 {
		t.Fatalf("volume 1 probed %d times, want <= %d", v1Fetches, maxSizeProbesPerFile+3)
	}
}

// Content-member sizing has the same bound: a member whose leading articles
// all 430 on Body falls back to the release part size after the cap instead
// of walking every article.
func TestSizeArticlesCapsProbes(t *testing.T) {
	const (
		partSize = 700_000
		nSegs    = 40
		length   = int64(nSegs-1)*partSize + 120_000
	)
	idx, fileID := sizingTestIndex(t, "v.rar", length)

	entry := sizingTestEntry(nSegs, "v")
	// No article is fetchable: every probe 430s on the wire.
	fetch := &countingFetcher{inner: &fakeFetcher{articles: map[string][]byte{}}}

	sf, _, err := sizeArticles(context.Background(), idx, fileID, entry, map[string]bool{},
		fetch, newArticleCache(64), partSize)
	if err != nil {
		t.Fatal(err)
	}
	if sf.SizeSource != SizeBorrowedHint {
		t.Fatalf("size source = %v, want borrowed_hint", sf.SizeSource)
	}
	if got := fetch.total(); got > maxSizeProbesPerFile {
		t.Fatalf("probed %d articles, want <= %d", got, maxSizeProbesPerFile)
	}
}

// ResolveFromNzb must flip progress to the planning stage BEFORE the PAR2
// sizing probes: sizing is minutes of fetches, and until now the UI sat on
// "checking 100%" the whole time.
func TestResolveFromNzbReportsPlanningBeforeSizingProbes(t *testing.T) {
	n, fetch, _, deadID := mkEncodedNzbFixture(t, 600, false)

	// Make the first PAR2 probe fail with a transient (non-430) error so
	// resolve aborts inside sizing — before planSet, which used to be the
	// first place the planning stage was reported.
	failing := &probeFailFetcher{inner: fetch}

	var stages []Stage
	progress := func(stage Stage, done, total int) { stages = append(stages, stage) }

	_, err := ResolveFromNzb(context.Background(), n, []string{deadID}, failing,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}, testLogger(), progress)
	if err == nil {
		t.Fatal("expected resolve to fail inside sizing")
	}
	for _, s := range stages {
		if s == StagePlanning {
			return
		}
	}
	t.Fatalf("stages = %v, want StagePlanning reported before sizing probes", stages)
}

// probeFailFetcher fails every par2 article fetch with a transient error.
type probeFailFetcher struct{ inner *fakeFetcher }

func (p *probeFailFetcher) Fetch(ctx context.Context, id string) ([]byte, error) {
	if len(id) >= 4 && id[:4] == "par2" {
		return nil, errors.New("nntp: all providers exhausted")
	}
	return p.inner.Fetch(ctx, id)
}

// sizingTestIndex builds a minimal PAR2 index with one file descriptor.
func sizingTestIndex(t *testing.T, name string, length int64) (*par2.Index, [16]byte) {
	t.Helper()
	idx := &par2.Index{SliceSize: 1 << 20, Files: map[[16]byte]par2.FileDescriptor{}}
	var fileID [16]byte
	fileID[0] = 7
	idx.Files[fileID] = par2.FileDescriptor{Name: name, Length: uint64(length)}
	return idx, fileID
}

// sizingTestEntry builds an NzbFileEntry with n sequential segments.
func sizingTestEntry(n int, prefix string) *metapb.NzbFileEntry {
	entry := &metapb.NzbFileEntry{}
	for j := range n {
		entry.Segments = append(entry.Segments, &metapb.NzbSeg{
			Id:     fmt.Sprintf("%s-%d@test", prefix, j),
			Number: int32(j + 1),
		})
	}
	return entry
}
