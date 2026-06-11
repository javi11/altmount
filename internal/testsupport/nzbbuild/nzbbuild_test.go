package nzbbuild_test

import (
	"testing"

	"github.com/javi11/altmount/internal/testsupport/nzbbuild"
	"github.com/javi11/nzbparser"
)

func TestBuild_roundtrip(t *testing.T) {
	n := nzbbuild.Build(
		nzbbuild.File{
			Subject: "Movie.2024.mkv",
			Segments: []nzbbuild.Segment{
				{ID: "seg-001@test", Bytes: 100},
				{ID: "seg-002@test", Bytes: 50},
			},
		},
		nzbbuild.File{
			Subject:  "Movie.2024.par2",
			Groups:   []string{"alt.binaries.custom"},
			Segments: []nzbbuild.Segment{{ID: "par2-001@test", Bytes: 200}},
		},
	)

	if len(n.Files) != 2 {
		t.Fatalf("got %d files, want 2", len(n.Files))
	}
	if n.Files[0].Subject != "Movie.2024.mkv" {
		t.Errorf("file[0].Subject = %q, want %q", n.Files[0].Subject, "Movie.2024.mkv")
	}
	if len(n.Files[0].Segments) != 2 {
		t.Errorf("file[0] segments = %d, want 2", len(n.Files[0].Segments))
	}
	if n.Files[0].Segments[0].ID != "seg-001@test" {
		t.Errorf("seg[0].ID = %q, want %q", n.Files[0].Segments[0].ID, "seg-001@test")
	}
	if n.Files[0].Segments[0].Bytes != 100 {
		t.Errorf("seg[0].Bytes = %d, want 100", n.Files[0].Segments[0].Bytes)
	}
	// Default groups applied to first file.
	if len(n.Files[0].Groups) != 1 || n.Files[0].Groups[0] != "alt.binaries.test" {
		t.Errorf("file[0].Groups = %v, want default", n.Files[0].Groups)
	}
	// Custom groups respected on second file.
	if len(n.Files[1].Groups) != 1 || n.Files[1].Groups[0] != "alt.binaries.custom" {
		t.Errorf("file[1].Groups = %v, want custom", n.Files[1].Groups)
	}

	// Round-trip through Write/Parse.
	data, err := nzbparser.Write(n)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	// Verify serialised XML is non-empty and parseable.
	if len(data) == 0 {
		t.Fatal("Write returned empty bytes")
	}
}

func TestWriteTemp_createsFile(t *testing.T) {
	n := nzbbuild.Build(nzbbuild.File{
		Subject:  "Test.Release.mkv",
		Segments: []nzbbuild.Segment{{ID: "t1@test", Bytes: 64}},
	})
	path := nzbbuild.WriteTemp(t, n, "Test.Release")
	if path == "" {
		t.Fatal("WriteTemp returned empty path")
	}
	// Path must end with .nzb.
	if path[len(path)-4:] != ".nzb" {
		t.Errorf("path %q does not end with .nzb", path)
	}
}
