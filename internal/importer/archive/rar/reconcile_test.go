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

// TestGroupArchivesReconcilesObfuscatedVolumeSet reproduces finding.you.2021…w4k:
// a 98-volume set where every volume carries a DISTINCT obfuscated base name
// (abc.xyz.<hash>.rNN) plus one headerless first volume whose name never resolved
// (it keeps its raw subject). Grouping by base name shatters this into 98 single-file
// groups, so only the first volume analyzes — producing a .mkv whose declared 14.6 GB
// size is backed by a single ~150 MB volume of segments. The reconciliation must fold
// all 98 back into one ordered set named .rar + .r00..r96 so rardecode follows it whole.
func TestGroupArchivesReconcilesObfuscatedVolumeSet(t *testing.T) {
	var files []parser.ParsedFile
	// The headerless first volume: unresolved obfuscated subject, no recognizable ordinal.
	files = append(files, parser.ParsedFile{
		Filename:      `[PRiVATE] \8617835f07\::8f72d6203509d9::/398c9b3ba773/`,
		OriginalIndex: 0,
	})
	// 97 continuations r00..r96, each with its own distinct obfuscated base.
	for i := 0; i <= 96; i++ {
		files = append(files, parser.ParsedFile{
			Filename:      fmt.Sprintf("abc.xyz.%014x.r%02d", i*0x1111+1, i),
			OriginalIndex: i + 1,
		})
	}

	groups := GroupArchivesByBaseName(files)

	if len(groups) != 1 {
		t.Fatalf("got %d groups; want 1 (single obfuscated volume set)", len(groups))
	}
	g := groups[0]
	if len(g) != 98 {
		t.Fatalf("merged group has %d volumes; want 98 (no volume dropped)", len(g))
	}

	// First volume → <base>.rar (header carrier); continuations → <base>.r00..r96 in order.
	if g[0].Filename != obfuscatedUnifiedBase+".rar" {
		t.Errorf("first volume = %q; want %q", g[0].Filename, obfuscatedUnifiedBase+".rar")
	}
	if g[1].Filename != obfuscatedUnifiedBase+".r00" {
		t.Errorf("second volume = %q; want %q", g[1].Filename, obfuscatedUnifiedBase+".r00")
	}
	if last := g[len(g)-1].Filename; last != obfuscatedUnifiedBase+".r96" {
		t.Errorf("last volume = %q; want %q", last, obfuscatedUnifiedBase+".r96")
	}

	// rardecode must pick the .rar as the first part to read the main header.
	rh := &rarProcessor{log: slog.Default()}
	gnames := make([]string, len(g))
	for i, f := range g {
		gnames[i] = f.Filename
	}
	first, err := rh.getFirstRarPart(gnames)
	if err != nil || first != obfuscatedUnifiedBase+".rar" {
		t.Errorf("getFirstRarPart = %q, err=%v; want %q", first, err, obfuscatedUnifiedBase+".rar")
	}
}

// TestReconcileObfuscatedLeavesDistinctSinglesAlone guards against merging genuinely
// unrelated single-volume archives that merely happen to share no base. Files with no
// contiguous ordinal run must stay separate.
func TestReconcileObfuscatedLeavesDistinctSinglesAlone(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "MovieA.rar"},
		{Filename: "MovieB.rar"},
		{Filename: "MovieC.rar"},
	}
	groups := GroupArchivesByBaseName(files)
	if len(groups) != 3 {
		t.Fatalf("got %d groups; want 3 (unrelated single archives must stay separate)", len(groups))
	}
}
