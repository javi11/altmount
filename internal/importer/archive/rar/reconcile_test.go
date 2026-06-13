package rar

import (
	"fmt"
	"log/slog"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
)

// TestGroupArchivesReconcilesRepostedFirstVolume reproduces the real-world failure
// where one physical RAR set has its first volume reposted under a different base
// name (Zero.Effect...monkee.repost.part01.rar) while every continuation volume keeps
// the original base (Zero.Effect...monkee.r00..r99 / .s00..s12, with no plain .rar).
//
// Grouping by base name splits this into the lone part01.rar group plus continuation
// groups, so only part01.rar (the one volume with a real archive header) ever gets
// mapped — exactly one ~100k volume, which is why streaming reported far fewer bytes
// than the file size. The reconciliation must fold everything into one set and rename
// the reposted first volume to <base>.rar so rardecode follows the whole sequence.
func TestGroupArchivesReconcilesRepostedFirstVolume(t *testing.T) {
	const base = "Zero.Effect.1998.1080p.AMZN.WEBRip.DD5.1.x264-monkee"

	var names []string
	names = append(names, base+".repost.part01.rar") // reposted first volume, different base
	for i := 0; i <= 99; i++ {
		names = append(names, fmt.Sprintf("%s.r%02d", base, i))
	}
	for i := 0; i <= 12; i++ {
		names = append(names, fmt.Sprintf("%s.s%02d", base, i))
	}

	files := make([]parser.ParsedFile, len(names))
	for i, n := range names {
		files[i] = parser.ParsedFile{Filename: n, OriginalIndex: i}
	}

	groups := GroupArchivesByBaseName(files)

	if len(groups) != 1 {
		t.Fatalf("got %d groups; want 1 (single physical RAR set)", len(groups))
	}
	g := groups[0]
	if len(g) != len(names) {
		t.Fatalf("merged group has %d volumes; want %d (no volume dropped)", len(g), len(names))
	}

	// The reposted first volume must be renamed to <continuation-base>.rar so the set
	// shares one base and rardecode's old-style volume following reaches every part.
	want := base + ".rar"
	if g[0].Filename != want {
		t.Errorf("first volume = %q; want %q", g[0].Filename, want)
	}
	if g[1].Filename != base+".r00" {
		t.Errorf("second volume = %q; want %q (continuations ordered by volume number)", g[1].Filename, base+".r00")
	}
	if last := g[len(g)-1].Filename; last != base+".s12" {
		t.Errorf("last volume = %q; want %q", last, base+".s12")
	}

	rh := &rarProcessor{log: slog.Default()}
	gnames := make([]string, len(g))
	for i, f := range g {
		gnames[i] = f.Filename
	}
	first, err := rh.getFirstRarPart(gnames)
	if err != nil || first != want {
		t.Errorf("getFirstRarPart = %q, err=%v; want %q", first, err, want)
	}
}

// TestReconcileLeavesSeparateArchivesAlone guards the conservative gate: two genuinely
// independent archives (each with its own first volume) must NOT be merged.
func TestReconcileLeavesSeparateArchivesAlone(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "MovieA.rar"}, {Filename: "MovieA.r00"}, {Filename: "MovieA.r01"},
		{Filename: "MovieB.rar"}, {Filename: "MovieB.r00"}, {Filename: "MovieB.r01"},
	}
	groups := GroupArchivesByBaseName(files)
	if len(groups) != 2 {
		t.Fatalf("got %d groups; want 2 (two independent archives must stay separate)", len(groups))
	}
}
