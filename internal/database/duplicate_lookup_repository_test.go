package database

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDuplicateLookupCatalogPersistence(t *testing.T) {
	db, err := NewDB(Config{DatabasePath: filepath.Join(t.TempDir(), "db")})
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	r := NewHealthRepository(db.Connection(), DialectSQLite)
	ctx := context.Background()
	entry := DuplicateCandidate{Path: "movies/m.mkv", Name: "m.mkv", Size: 3, Identity: "version1"}
	if err = r.UpsertDuplicateCandidate(ctx, entry); err != nil {
		t.Fatal(err)
	}
	if err = r.SetDuplicateFingerprint(ctx, entry.Path, entry.Identity, "hash"); err != nil {
		t.Fatal(err)
	}
	got, err := r.DuplicateCandidates(ctx, "m.mkv", 3)
	if err != nil || len(got) != 1 || got[0].Hash != "hash" {
		t.Fatalf("got=%v err=%v", got, err)
	}
	entry.Identity = "version2"
	if err = r.UpsertDuplicateCandidate(ctx, entry); err != nil {
		t.Fatal(err)
	}
	got, err = r.DuplicateCandidates(ctx, "m.mkv", 3)
	if err != nil || len(got) != 1 || got[0].Hash != "" {
		t.Fatalf("replacement kept fingerprint: %v %v", got, err)
	}
}
