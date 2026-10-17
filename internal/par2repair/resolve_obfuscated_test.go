package par2repair

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/testsupport/par2gen"
)

// obfuscatedRelease is a fake release whose subjects never name the PAR2
// set's files, so every recovery-set member must be matched by content.
type obfuscatedRelease struct {
	fm       *metapb.FileMetadata
	store    *metapb.NzbStore
	fetch    *fakeFetcher
	names    []string          // PAR2 member name of content file i
	contents map[string][]byte // by PAR2 member name
	heads    []string          // head article ID of content file i
}

const (
	obfFileSize = 32 << 10
	obfArtSize  = 16 << 10
)

// mkObfuscatedRelease builds numFiles content files of obfFileSize bytes,
// posted under subjects that carry no filename, plus a full PAR2 set. mutate,
// when set, may edit file contents before the PAR2 set is computed.
func mkObfuscatedRelease(t *testing.T, numFiles int, mutate func(contents [][]byte)) *obfuscatedRelease {
	t.Helper()
	rng := rand.New(rand.NewSource(3))
	raw := make([][]byte, numFiles)
	for i := range raw {
		raw[i] = make([]byte, obfFileSize)
		rng.Read(raw[i])
	}
	if mutate != nil {
		mutate(raw)
	}
	r := &obfuscatedRelease{
		fm:       &metapb.FileMetadata{},
		store:    &metapb.NzbStore{},
		fetch:    &fakeFetcher{articles: map[string][]byte{}},
		contents: map[string][]byte{},
	}
	var entries []par2gen.FileEntry
	for i, b := range raw {
		name := fmt.Sprintf("x.part%03d.rar", i+1)
		r.names = append(r.names, name)
		r.contents[name] = b
		entries = append(entries, par2gen.FileEntry{Name: name, Content: b})
	}
	set := par2gen.BuildFull(obfArtSize, entries, 4)

	for i, name := range r.names {
		entry := &metapb.NzbFileEntry{Subject: fmt.Sprintf("obf%dqwerty [%d/%d] yEnc (1/2)", i, i+1, numFiles)}
		for a := 0; a*obfArtSize < obfFileSize; a++ {
			id := fmt.Sprintf("f%d-%d@test", i, a)
			entry.Segments = append(entry.Segments, &metapb.NzbSeg{Id: id, Number: int32(a + 1), Bytes: obfArtSize + 300})
			r.fetch.articles[id] = r.contents[name][a*obfArtSize : (a+1)*obfArtSize]
			if a == 0 {
				r.heads = append(r.heads, id)
			}
		}
		// Prepend: the NZB lists files in the reverse of the PAR2 set's
		// order, so a positional match can never pass by accident.
		r.store.Files = append([]*metapb.NzbFileEntry{entry}, r.store.Files...)
	}
	for i, p := range append([][]byte{set.Index}, set.Volumes...) {
		id := fmt.Sprintf("par2-%d@test", i)
		r.fetch.articles[id] = p
		r.store.Files = append(r.store.Files, &metapb.NzbFileEntry{
			Subject:  fmt.Sprintf("obfpar%d [%d/9] yEnc (1/1)", i, i),
			Segments: []*metapb.NzbSeg{{Id: id, Number: 1, Bytes: int64(len(p))}},
		})
		r.fm.Par2Files = append(r.fm.Par2Files, &metapb.Par2FileReference{
			Filename:    fmt.Sprintf("p%d.par2", i),
			FileSize:    int64(len(p)),
			SegmentData: []*metapb.SegmentData{{Id: id, SegmentSize: int64(len(p))}},
		})
	}
	return r
}

// kill makes an article missing on every provider.
func (r *obfuscatedRelease) kill(id string) { delete(r.fetch.articles, id) }

func (r *obfuscatedRelease) resolve(ctx context.Context, dead ...string) (*Resolution, error) {
	return Resolve(ctx, r.fm, r.store, dead, r.fetch,
		Caps{MaxRepairRatio: 0.5, MaxMemoryBytes: 64 << 20}, testLogger(), nil)
}

// assertMatchedToOwnArticles checks every planned member was paired with
// the NZB entry that really carries its bytes. All members share one size,
// so a wrong pairing would otherwise pass every size check.
func (r *obfuscatedRelease) assertMatchedToOwnArticles(t *testing.T, res *Resolution) {
	t.Helper()
	for _, f := range res.Plan.Files {
		name := res.Index.Files[f.FileID].Name
		i := -1
		for j, n := range r.names {
			if n == name {
				i = j
			}
		}
		if i < 0 {
			t.Fatalf("planned member %q is not in the release", name)
		}
		if got, want := f.Articles[0].MessageID, r.heads[i]; got != want {
			t.Fatalf("member %q matched to articles starting %s, want %s", name, got, want)
		}
	}
}

// An obfuscated release is matched by content. Each NZB file's head article
// is fetched exactly once — by the head index, then reused for sizing — and
// every member is paired with its own articles. The old matcher re-fetched
// every candidate's head for every member: quadratic, and with more files
// than the article cache holds it left a 264-volume release in planning for
// hours.
func TestResolveObfuscatedReleaseFetchesHeadsOnce(t *testing.T) {
	const numFiles = 100 // > resolveCacheCap, so the cache cannot hide a rescan
	r := mkObfuscatedRelease(t, numFiles, nil)
	deadID := "f0-1@test"
	r.kill(deadID)

	res, err := r.resolve(context.Background(), deadID)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Files) != numFiles {
		t.Fatalf("planned %d members, want %d", len(res.Plan.Files), numFiles)
	}
	if len(res.Plan.DeadArticles) != 1 || res.Plan.DeadArticles[0].MessageID != deadID {
		t.Fatalf("dead articles = %+v, want only %s", res.Plan.DeadArticles, deadID)
	}
	r.assertMatchedToOwnArticles(t, res)
	total, worst, worstID := 0, 0, ""
	for _, id := range r.heads {
		n := r.fetch.countFetches(id)
		total += n
		if n > worst {
			worst, worstID = n, id
		}
	}
	if total != numFiles {
		t.Fatalf("head articles fetched %d times in total for %d files (worst: %s, %d times); want each exactly once",
			total, numFiles, worstID, worst)
	}
}

// Two members whose first 16 KiB are identical share a Hash16k. Each must
// still claim its own NZB entry rather than both landing on the first.
func TestResolveObfuscatedReleaseWithIdenticalHeads(t *testing.T) {
	r := mkObfuscatedRelease(t, 6, func(contents [][]byte) {
		copy(contents[4][:16384], contents[1][:16384])
	})
	r.kill("f2-1@test")
	res, err := r.resolve(context.Background(), "f2-1@test")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Plan.Files) != 6 {
		t.Fatalf("planned %d members, want 6", len(res.Plan.Files))
	}
	seen := map[string]bool{}
	for _, f := range res.Plan.Files {
		head := f.Articles[0].MessageID
		if seen[head] {
			t.Fatalf("two members matched to the same NZB entry (head %s)", head)
		}
		seen[head] = true
	}
}

// A member whose head article is dead cannot be matched by content (its
// name is in no subject either), so the plan fails with an unrepairable
// verdict naming it, as before; the head index skips the dead entry instead
// of asking a provider for it.
func TestResolveObfuscatedReleaseDeadHeadIsUnrepairable(t *testing.T) {
	r := mkObfuscatedRelease(t, 4, nil)
	r.kill(r.heads[2])

	_, err := r.resolve(context.Background(), r.heads[2])
	if !errors.Is(err, ErrUnrepairable) || !strings.Contains(err.Error(), r.names[2]) {
		t.Fatalf("err = %v, want unrepairable naming %s", err, r.names[2])
	}
	if n := r.fetch.countFetches(r.heads[2]); n != 0 {
		t.Fatalf("known-dead head fetched %d times", n)
	}
}

// A cancelled job stops matching promptly with the context's error instead
// of grinding through the remaining heads.
func TestResolveObfuscatedReleaseStopsOnCancel(t *testing.T) {
	r := mkObfuscatedRelease(t, 40, nil)
	r.kill("f0-1@test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := r.resolve(ctx, "f0-1@test"); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}
