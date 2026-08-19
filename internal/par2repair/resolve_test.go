package par2repair

import (
	"bytes"
	"context"
	"fmt"
	"math/rand"
	"testing"

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

func TestResolveNoPar2Files(t *testing.T) {
	_, store, fetch, _, _ := mkResolveFixture(t, false)
	fm := &metapb.FileMetadata{}
	_, err := Resolve(context.Background(), fm, store, nil, fetch, Caps{})
	if err == nil {
		t.Fatal("want error without par2 files")
	}
}
