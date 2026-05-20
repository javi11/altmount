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

func TestIsoFileContentToNestedSource(t *testing.T) {
	t.Parallel()

	t.Run("unencrypted uses pre-sliced segments", func(t *testing.T) {
		t.Parallel()
		segs := []*metapb.SegmentData{
			{Id: "a", StartOffset: 0, EndOffset: 99, SegmentSize: 100},
		}
		fc := iso.ISOFileContent{
			Filename: "00001.m2ts",
			Size:     100,
			Segments: segs,
		}
		ns := isoFileContentToNestedSource(fc)
		if len(ns.Segments) != 1 || ns.InnerLength != 100 || ns.InnerOffset != 0 {
			t.Fatalf("unexpected NestedSource: %+v", ns)
		}
		if len(ns.AesKey) != 0 {
			t.Errorf("AesKey should be empty, got %v", ns.AesKey)
		}
	})

	t.Run("encrypted carries offset and key", func(t *testing.T) {
		t.Parallel()
		segs := []*metapb.SegmentData{
			{Id: "outer", StartOffset: 0, EndOffset: 99999, SegmentSize: 100000},
		}
		fc := iso.ISOFileContent{
			Filename: "00001.m2ts",
			Size:     2048,
			NestedSource: &iso.ISONestedSource{
				Segments:        segs,
				AesKey:          []byte("0123456789abcdef0123456789abcdef"),
				AesIV:           []byte("0123456789abcdef"),
				InnerOffset:     1024,
				InnerLength:     2048,
				InnerVolumeSize: 99999,
			},
		}
		ns := isoFileContentToNestedSource(fc)
		if ns.InnerOffset != 1024 || ns.InnerLength != 2048 || ns.InnerVolumeSize != 99999 {
			t.Fatalf("unexpected NestedSource offsets: %+v", ns)
		}
		if len(ns.AesKey) == 0 {
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
			Segments: []*metapb.SegmentData{
				{Id: name, StartOffset: 0, EndOffset: size - 1, SegmentSize: size},
			},
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

func TestBuildLargestFileContent(t *testing.T) {
	t.Parallel()

	files := []iso.ISOFileContent{
		{Filename: "small.mkv", Size: 500, Segments: []*metapb.SegmentData{
			{Id: "s", StartOffset: 0, EndOffset: 499, SegmentSize: 500},
		}},
		{Filename: "big.mkv", Size: 5_000_000, Segments: []*metapb.SegmentData{
			{Id: "b", StartOffset: 0, EndOffset: 4_999_999, SegmentSize: 5_000_000},
		}},
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
