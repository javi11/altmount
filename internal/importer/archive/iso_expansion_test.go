package archive

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/importer/archive/iso"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

func TestDiscGroupKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		label    string
		filename string
		wantKey  string
		wantNum  int
	}{
		{"avatar disc 1 label", "AVATAR_FIRE_AND_ASH_DISC_1", "any.iso", "AVATAR_FIRE_AND_ASH", 1},
		{"avatar disc 2 label", "AVATAR_FIRE_AND_ASH_DISC_2", "any.iso", "AVATAR_FIRE_AND_ASH", 2},
		{"compact DISC2", "MOVIE_DISC2", "any.iso", "MOVIE", 2},
		{"CD suffix", "MOVIE-CD1", "any.iso", "MOVIE", 1},
		{"PART suffix with spaces", "TITLE PART 3", "any.iso", "TITLE", 3},
		{"letter disc identifier B → 2", "FOO_DISC_B", "any.iso", "FOO", 2},
		{"no suffix → solo", "PLAIN_MOVIE", "any.iso", "PLAIN_MOVIE", 0},
		{"empty label falls back to filename stem", "", "MyMovie_Disc_1.iso", "MYMOVIE", 1},
		{"empty label and weird filename", "", "thing.iso", "THING", 0},
		{"only label has disc, filename plain", "X_DISC_2", "anything.iso", "X", 2},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			gotKey, gotNum := discGroupKey(tc.label, tc.filename)
			if gotKey != tc.wantKey || gotNum != tc.wantNum {
				t.Errorf("discGroupKey(%q,%q) = (%q,%d), want (%q,%d)",
					tc.label, tc.filename, gotKey, gotNum, tc.wantKey, tc.wantNum)
			}
		})
	}
}

func TestParseDiscNumber(t *testing.T) {
	t.Parallel()

	cases := map[string]int{
		"1":     1,
		"2":     2,
		"10":    10,
		"A":     1,
		"a":     1,
		"B":     2,
		"":      0,
		"AB":    0,
		"foo":   0,
	}
	for in, want := range cases {
		if got := parseDiscNumber(in); got != want {
			t.Errorf("parseDiscNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestIsoFileContentToNestedSources(t *testing.T) {
	t.Parallel()

	t.Run("single unencrypted extent → one NestedSource", func(t *testing.T) {
		t.Parallel()
		fc := iso.ISOFileContent{
			Filename: "00001.m2ts",
			Size:     100,
			Sources: []iso.ISONestedSource{{
				Segments:        []*metapb.SegmentData{{Id: "a", StartOffset: 0, EndOffset: 99, SegmentSize: 100}},
				InnerOffset:     0,
				InnerLength:     100,
				InnerVolumeSize: 100,
			}},
		}
		got := isoFileContentToNestedSources(fc)
		if len(got) != 1 {
			t.Fatalf("want 1 source, got %d", len(got))
		}
		if got[0].InnerLength != 100 || got[0].InnerOffset != 0 || len(got[0].AesKey) != 0 {
			t.Fatalf("unexpected NestedSource: %+v", got[0])
		}
	})

	t.Run("multi-extent file → one NestedSource per extent in order", func(t *testing.T) {
		t.Parallel()
		// The bug we just fixed: a 17 GiB M2TS spans hundreds of extents.
		// Each extent must become its own NestedSource so the downstream
		// concat reader stitches them in disc order.
		fc := iso.ISOFileContent{
			Filename: "00022.m2ts",
			Size:     30,
			Sources: []iso.ISONestedSource{
				{Segments: []*metapb.SegmentData{{Id: "e1"}}, InnerLength: 10},
				{Segments: []*metapb.SegmentData{{Id: "e2"}}, InnerLength: 10},
				{Segments: []*metapb.SegmentData{{Id: "e3"}}, InnerLength: 10},
			},
		}
		got := isoFileContentToNestedSources(fc)
		if len(got) != 3 {
			t.Fatalf("want 3 sources, got %d", len(got))
		}
		wantIDs := []string{"e1", "e2", "e3"}
		for i, ns := range got {
			if len(ns.Segments) != 1 || ns.Segments[0].Id != wantIDs[i] {
				t.Errorf("source %d: want segment id %q, got %+v", i, wantIDs[i], ns.Segments)
			}
		}
	})

	t.Run("encrypted source carries key + IV through", func(t *testing.T) {
		t.Parallel()
		fc := iso.ISOFileContent{
			Filename: "00001.m2ts",
			Size:     2048,
			Sources: []iso.ISONestedSource{{
				Segments:        []*metapb.SegmentData{{Id: "outer", StartOffset: 0, EndOffset: 99999, SegmentSize: 100000}},
				AesKey:          []byte("0123456789abcdef0123456789abcdef"),
				AesIV:           []byte("0123456789abcdef"),
				InnerOffset:     1024,
				InnerLength:     2048,
				InnerVolumeSize: 99999,
			}},
		}
		got := isoFileContentToNestedSources(fc)
		if len(got) != 1 {
			t.Fatalf("want 1 source, got %d", len(got))
		}
		if got[0].InnerOffset != 1024 || got[0].InnerLength != 2048 || got[0].InnerVolumeSize != 99999 {
			t.Fatalf("offsets mangled: %+v", got[0])
		}
		if len(got[0].AesKey) == 0 {
			t.Error("AesKey should be carried through for encrypted source")
		}
	})
}

func TestBuildMainFeatureContent_TwoDiscs(t *testing.T) {
	t.Parallel()

	// Helper to make a fake ISO main-feature ISOFileContent with given size
	// and a single-segment outer slice (segment values are not interpreted
	// by buildMainFeatureContent — only Size and the source attributes
	// matter for the assembled NestedSources chain).
	mkClip := func(name string, size int64) iso.ISOFileContent {
		return iso.ISOFileContent{
			Filename: name,
			Size:     size,
			Sources: []iso.ISONestedSource{{
				Segments: []*metapb.SegmentData{
					{Id: name, StartOffset: 0, EndOffset: size - 1, SegmentSize: size},
				},
				InnerLength:     size,
				InnerVolumeSize: size,
			}},
		}
	}

	disc1 := analyzedISO{
		src: Content{Filename: "AVATAR_DISC_1.iso", NzbdavID: "nzb-1"},
		analyzed: &iso.AnalyzedISO{
			VolumeLabel: "AVATAR_DISC_1",
			MainFeature: []iso.ISOFileContent{
				mkClip("00001.m2ts", 10_000_000),
				mkClip("00002.m2ts", 20_000_000),
			},
		},
		discNum:  1,
		groupKey: "AVATAR",
	}
	disc2 := analyzedISO{
		src: Content{Filename: "AVATAR_DISC_2.iso", NzbdavID: "nzb-2"},
		analyzed: &iso.AnalyzedISO{
			VolumeLabel: "AVATAR_DISC_2",
			MainFeature: []iso.ISOFileContent{
				mkClip("00003.m2ts", 30_000_000),
			},
		},
		discNum:  2,
		groupKey: "AVATAR",
	}

	got, ok := buildMainFeatureContent(context.Background(), "AVATAR", []analyzedISO{disc1, disc2})
	if !ok {
		t.Fatal("buildMainFeatureContent returned ok=false")
	}
	if got.ISOExpansionIndex != 1 {
		t.Errorf("ISOExpansionIndex = %d, want 1", got.ISOExpansionIndex)
	}
	if got.NzbdavID != "nzb-1" {
		t.Errorf("NzbdavID = %q, want nzb-1 (from first disc)", got.NzbdavID)
	}
	if len(got.NestedSources) != 3 {
		t.Fatalf("NestedSources count = %d, want 3 (2 clips from disc 1 + 1 clip from disc 2)", len(got.NestedSources))
	}
	wantSize := int64(10_000_000 + 20_000_000 + 30_000_000)
	if got.Size != wantSize {
		t.Errorf("Size = %d, want %d", got.Size, wantSize)
	}
	if got.PackedSize != wantSize {
		t.Errorf("PackedSize = %d, want %d", got.PackedSize, wantSize)
	}
	// Order must follow disc-then-playlist (disc1.clip1, disc1.clip2, disc2.clip3).
	wantOrder := []int64{10_000_000, 20_000_000, 30_000_000}
	for i, ns := range got.NestedSources {
		if ns.InnerLength != wantOrder[i] {
			t.Errorf("NestedSources[%d].InnerLength = %d, want %d", i, ns.InnerLength, wantOrder[i])
		}
	}
	if got.Filename != "AVATAR.m2ts" {
		t.Errorf("Filename = %q, want AVATAR.m2ts", got.Filename)
	}
}

// TestBuildMainFeatureContent_ClipBoundaries verifies the continuous-timeline
// table: each clip's Delta90k lifts its native PTS base (InTimeTicks*2) onto a
// running 90 kHz timeline that advances by each clip's authored span
// (DurationTicks*2), with clip 0 keeping its native base (delta 0). The table
// spans both discs in output order.
func TestBuildMainFeatureContent_ClipBoundaries(t *testing.T) {
	t.Parallel()

	// mkTimed builds a one-extent clip with MPLS timing (45 kHz).
	mkTimed := func(name string, size, inTime, durTicks int64) iso.ISOFileContent {
		return iso.ISOFileContent{
			Filename: name,
			Size:     size,
			Sources: []iso.ISONestedSource{{
				Segments:    []*metapb.SegmentData{{Id: name, EndOffset: size - 1, SegmentSize: size}},
				InnerLength: size,
			}},
			InTimeTicks:   inTime,
			DurationTicks: durTicks,
		}
	}

	disc1 := analyzedISO{
		src:      Content{Filename: "M_DISC_1.iso", NzbdavID: "nzb-1"},
		groupKey: "M", discNum: 1,
		analyzed: &iso.AnalyzedISO{MainFeature: []iso.ISOFileContent{
			mkTimed("00001.m2ts", 10_000_000, 1000, 45000), // base90k 2000, span90k 90000
			mkTimed("00002.m2ts", 20_000_000, 500, 90000),  // base90k 1000, span90k 180000
		}},
	}
	disc2 := analyzedISO{
		src:      Content{Filename: "M_DISC_2.iso", NzbdavID: "nzb-2"},
		groupKey: "M", discNum: 2,
		analyzed: &iso.AnalyzedISO{MainFeature: []iso.ISOFileContent{
			mkTimed("00003.m2ts", 30_000_000, 0, 45000), // base90k 0, span90k 90000
		}},
	}

	got, ok := buildMainFeatureContent(context.Background(), "M", []analyzedISO{disc1, disc2})
	if !ok {
		t.Fatal("ok=false")
	}
	if len(got.ClipBoundaries) != 3 {
		t.Fatalf("ClipBoundaries = %d, want 3", len(got.ClipBoundaries))
	}

	// base0_90k = 2000.
	// clip0: tlStart 2000, delta 0;          cum→90000
	// clip1: tlStart 2000+90000=92000, delta 92000-1000=91000; cum→270000
	// clip2: tlStart 2000+270000=272000, delta 272000-0=272000
	wantByteLen := []int64{10_000_000, 20_000_000, 30_000_000}
	wantDelta := []int64{0, 91000, 272000}
	for i, cb := range got.ClipBoundaries {
		if cb.ByteLen != wantByteLen[i] {
			t.Errorf("clip %d ByteLen = %d, want %d", i, cb.ByteLen, wantByteLen[i])
		}
		if cb.Delta90k != wantDelta[i] {
			t.Errorf("clip %d Delta90k = %d, want %d", i, cb.Delta90k, wantDelta[i])
		}
	}

	// Σ ByteLen must equal the file size, so the boundary prefix-sums align
	// exactly with the flattened NestedSources byte layout.
	var sumBytes int64
	for _, cb := range got.ClipBoundaries {
		sumBytes += cb.ByteLen
	}
	if sumBytes != got.Size {
		t.Errorf("Σ ClipBoundaries.ByteLen = %d, want file size %d", sumBytes, got.Size)
	}
}

// TestBuildMainFeatureContent_NoTimingNoBoundaries: when MPLS timing is absent
// (all zero), no clip-boundary table is attached, so the read-path remux
// filter stays disabled and the file is served as a plain byte concatenation.
func TestBuildMainFeatureContent_NoTimingNoBoundaries(t *testing.T) {
	t.Parallel()
	mkClip := func(name string, size int64) iso.ISOFileContent {
		return iso.ISOFileContent{
			Filename: name, Size: size,
			Sources: []iso.ISONestedSource{{
				Segments:    []*metapb.SegmentData{{Id: name, EndOffset: size - 1, SegmentSize: size}},
				InnerLength: size,
			}},
		}
	}
	d := analyzedISO{
		src: Content{Filename: "X.iso"}, groupKey: "X",
		analyzed: &iso.AnalyzedISO{MainFeature: []iso.ISOFileContent{mkClip("a", 100), mkClip("b", 200)}},
	}
	got, ok := buildMainFeatureContent(context.Background(), "X", []analyzedISO{d})
	if !ok {
		t.Fatal("ok=false")
	}
	if len(got.ClipBoundaries) != 0 {
		t.Errorf("ClipBoundaries = %d, want 0 when no MPLS timing present", len(got.ClipBoundaries))
	}
}

func TestBuildMainFeatureContent_ClipBoundaryByteSumGuard(t *testing.T) {
	t.Parallel()

	mkTimed := func(name string, declaredSize, sourceSize int64) iso.ISOFileContent {
		return iso.ISOFileContent{
			Filename: name,
			Size:     declaredSize,
			Sources: []iso.ISONestedSource{{
				Segments:    []*metapb.SegmentData{{Id: name, EndOffset: sourceSize - 1, SegmentSize: sourceSize}},
				InnerLength: sourceSize,
			}},
			InTimeTicks:   1000,
			DurationTicks: 45000,
		}
	}

	t.Run("matched byte sum preserves boundaries", func(t *testing.T) {
		t.Parallel()
		d := analyzedISO{
			src: Content{Filename: "M.iso"}, groupKey: "M",
			analyzed: &iso.AnalyzedISO{MainFeature: []iso.ISOFileContent{
				mkTimed("00001.m2ts", 100, 100),
				mkTimed("00002.m2ts", 200, 200),
			}},
		}

		got, ok := buildMainFeatureContent(context.Background(), "M", []analyzedISO{d})
		if !ok {
			t.Fatal("ok=false")
		}
		if len(got.ClipBoundaries) != 2 {
			t.Fatalf("ClipBoundaries = %d, want 2", len(got.ClipBoundaries))
		}
	})

	t.Run("mismatched byte sum disables boundaries", func(t *testing.T) {
		t.Parallel()
		d := analyzedISO{
			src: Content{Filename: "M.iso"}, groupKey: "M",
			analyzed: &iso.AnalyzedISO{MainFeature: []iso.ISOFileContent{
				mkTimed("00001.m2ts", 100, 100),
				mkTimed("00002.m2ts", 200, 150),
			}},
		}

		got, ok := buildMainFeatureContent(context.Background(), "M", []analyzedISO{d})
		if !ok {
			t.Fatal("ok=false")
		}
		if len(got.ClipBoundaries) != 0 {
			t.Fatalf("ClipBoundaries = %d, want 0 on byte sum mismatch", len(got.ClipBoundaries))
		}
	})
}

func TestBuildLargestFileContent(t *testing.T) {
	t.Parallel()

	mkFile := func(name string, size int64, segID string) iso.ISOFileContent {
		return iso.ISOFileContent{
			Filename: name,
			Size:     size,
			Sources: []iso.ISONestedSource{{
				Segments:        []*metapb.SegmentData{{Id: segID, StartOffset: 0, EndOffset: size - 1, SegmentSize: size}},
				InnerLength:     size,
				InnerVolumeSize: size,
			}},
		}
	}
	files := []iso.ISOFileContent{
		mkFile("small.mkv", 500, "s"),
		mkFile("big.mkv", 5_000_000, "b"),
	}
	src := Content{Filename: "thing.iso", NzbdavID: "id-1"}

	got, ok := buildLargestFileContent(src, files)
	if !ok {
		t.Fatal("buildLargestFileContent returned ok=false")
	}
	if got.Filename != "big.mkv" {
		t.Errorf("Filename = %q, want big.mkv (largest)", got.Filename)
	}
	if got.ISOExpansionIndex != 1 {
		t.Errorf("ISOExpansionIndex = %d, want 1", got.ISOExpansionIndex)
	}
	if got.NzbdavID != "id-1" {
		t.Errorf("NzbdavID = %q, want id-1", got.NzbdavID)
	}
}
