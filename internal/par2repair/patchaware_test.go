package par2repair

import (
	"context"
	"errors"
	"testing"

	"github.com/javi11/nntppool/v4"
)

// statFakeFetcher is a fakeFetcher that also implements ArticleStater:
// articles absent from the map are missing.
type statFakeFetcher struct{ *fakeFetcher }

func (s *statFakeFetcher) StatIDs(ctx context.Context, ids []string, onResult func(done int)) (map[string]bool, error) {
	missing := map[string]bool{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, id := range ids {
		if _, ok := s.articles[id]; !ok {
			missing[id] = true
		}
		if onResult != nil {
			onResult(i + 1)
		}
	}
	return missing, nil
}

// A patched article is alive: Fetch serves the patch, StatIDs never reports
// it missing, and HasPatch says so — this is what stops a completed repair
// from being re-planned and re-downloaded on the next cycle.
func TestPatchAwareFetcherServesPatches(t *testing.T) {
	ps := NewPatchStore(t.TempDir())
	if err := ps.Put("dead@test", []byte("repaired-bytes")); err != nil {
		t.Fatal(err)
	}
	inner := &statFakeFetcher{fakeFetcher: &fakeFetcher{articles: map[string][]byte{
		"live@test": []byte("wire-bytes"),
	}}}
	f := newPatchAwareFetcher(inner, ps)

	got, err := f.Fetch(context.Background(), "dead@test")
	if err != nil || string(got) != "repaired-bytes" {
		t.Fatalf("Fetch(patched) = %q, %v; want the patch", got, err)
	}
	got, err = f.Fetch(context.Background(), "live@test")
	if err != nil || string(got) != "wire-bytes" {
		t.Fatalf("Fetch(live) = %q, %v; want wire bytes", got, err)
	}
	if _, err := f.Fetch(context.Background(), "gone@test"); err == nil {
		t.Fatal("Fetch(gone) succeeded, want error")
	}

	missing, err := f.StatIDs(context.Background(), []string{"dead@test", "live@test", "gone@test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing["dead@test"] {
		t.Fatal("patched article reported missing by StatIDs")
	}
	if !missing["gone@test"] {
		t.Fatal("truly gone article not reported missing")
	}
	if !f.HasPatch("dead@test") || f.HasPatch("live@test") {
		t.Fatal("HasPatch verdicts wrong")
	}
}

// ErrArticleNotFound must still surface for unpatched articles through the
// wrapper (the sweep's absorb logic keys on it).
func TestPatchAwareFetcherPassesThroughNotFound(t *testing.T) {
	ps := NewPatchStore(t.TempDir())
	inner := &statFakeFetcher{fakeFetcher: &fakeFetcher{articles: map[string][]byte{}}}
	f := newPatchAwareFetcher(inner, ps)

	_, err := f.Fetch(context.Background(), "gone@test")
	if err == nil || !isNotFound(err) {
		t.Fatalf("err = %v, want ErrArticleNotFound", err)
	}
}

func isNotFound(err error) bool {
	return errors.Is(err, nntppool.ErrArticleNotFound)
}
