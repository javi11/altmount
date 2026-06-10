package importer

import (
	"context"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

func TestParsedFileToISOContent_MapsAllFields(t *testing.T) {
	pf := parser.ParsedFile{
		Filename: "Movie_DISC_1.iso",
		Size:     42_949_672_960, // 40 GiB
		NzbdavID: "abc-123",
		Segments: []*metapb.SegmentData{
			{Id: "msg1@", StartOffset: 0, EndOffset: 750_000, SegmentSize: 750_000},
		},
		AesKey: []byte{0xAA, 0xBB},
		AesIv:  []byte{0xCC, 0xDD},
	}

	got := parsedFileToISOContent(pf)

	if got.Filename != "Movie_DISC_1.iso" {
		t.Errorf("Filename = %q, want Movie_DISC_1.iso", got.Filename)
	}
	if got.Size != 42_949_672_960 {
		t.Errorf("Size = %d, want 42949672960", got.Size)
	}
	if got.PackedSize != 42_949_672_960 {
		t.Errorf("PackedSize = %d, want 42949672960 (bare ISO is unpacked)", got.PackedSize)
	}
	if got.NzbdavID != "abc-123" {
		t.Errorf("NzbdavID = %q, want abc-123", got.NzbdavID)
	}
	if len(got.Segments) != 1 || got.Segments[0].Id != "msg1@" {
		t.Errorf("Segments not preserved: %#v", got.Segments)
	}
	if string(got.AesKey) != "\xAA\xBB" || string(got.AesIV) != "\xCC\xDD" {
		t.Errorf("AES key/IV not preserved")
	}
}

func TestPartitionISOFiles_SeparatesISOFromRest(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "Movie_DISC_1.iso"},
		{Filename: "readme.txt"},
		{Filename: "Movie_DISC_2.ISO"},
		{Filename: "extras.mkv"},
	}

	isos, rest := partitionISOFiles(files)

	if len(isos) != 2 {
		t.Fatalf("isos = %d, want 2", len(isos))
	}
	if isos[0].Filename != "Movie_DISC_1.iso" || isos[1].Filename != "Movie_DISC_2.ISO" {
		t.Errorf("isos = %+v", isos)
	}
	if len(rest) != 2 || rest[0].Filename != "readme.txt" || rest[1].Filename != "extras.mkv" {
		t.Errorf("rest = %+v", rest)
	}
}

func TestExpandBareISOFiles_NoISOs_ReturnsInputUntouched(t *testing.T) {
	files := []parser.ParsedFile{{Filename: "a.mkv"}, {Filename: "b.mp4"}}
	written, rest, err := expandBareISOFiles(context.Background(), expandBareISODeps{
		expand: func(ctx context.Context, _ bool, _ []archive.Content) ([]archive.Content, error) {
			t.Fatal("expand should not be called when no .iso present")
			return nil, nil
		},
	}, files, "vdir", "movie", "", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(written) != 0 {
		t.Errorf("written = %v, want []", written)
	}
	if len(rest) != 2 {
		t.Errorf("rest = %d, want 2", len(rest))
	}
}

func TestExpandBareISOFiles_OneISO_BluRayPath_WritesMergedMetadata(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "movie.iso", Size: 25_000_000_000},
		{Filename: "readme.txt"},
	}
	expandCalled := false
	deps := expandBareISODeps{
		expand: func(ctx context.Context, enabled bool, in []archive.Content) ([]archive.Content, error) {
			expandCalled = true
			if !enabled {
				t.Error("expand called with enabled=false")
			}
			if len(in) != 1 || in[0].Filename != "movie.iso" {
				t.Errorf("unexpected expand input: %+v", in)
			}
			return []archive.Content{{
				Filename: "MOVIE.m2ts",
				Size:     20_000_000_000,
				NestedSources: []archive.NestedSource{
					{InnerOffset: 0, InnerLength: 10_000_000_000},
					{InnerOffset: 0, InnerLength: 10_000_000_000},
				},
			}}, nil
		},
		writeMetadata: func(virtualPath string, _ *metapb.FileMetadata) error {
			if virtualPath != "vdir/MOVIE.m2ts" {
				t.Errorf("virtualPath = %q, want vdir/MOVIE.m2ts", virtualPath)
			}
			return nil
		},
		enabled: true,
	}

	written, rest, err := expandBareISOFiles(context.Background(), deps, files, "vdir", "movie", "", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if !expandCalled {
		t.Error("expand was never called")
	}
	if len(written) != 1 || written[0] != "vdir/MOVIE.m2ts" {
		t.Errorf("written = %v", written)
	}
	if len(rest) != 1 || rest[0].Filename != "readme.txt" {
		t.Errorf("rest = %v", rest)
	}
}

func TestExpandBareISOFiles_Disabled_StillPeelsButFallsBack(t *testing.T) {
	files := []parser.ParsedFile{{Filename: "movie.iso", Size: 1000}}
	deps := expandBareISODeps{
		enabled: false,
		expand: func(ctx context.Context, enabled bool, in []archive.Content) ([]archive.Content, error) {
			if enabled {
				t.Error("expand was called with enabled=true; want enabled=false")
			}
			// archive.ExpandISOContents with expand=false returns input unchanged.
			return in, nil
		},
		writeMetadata: func(string, *metapb.FileMetadata) error {
			t.Fatal("writeMetadata should not be called when bare ISO is unchanged")
			return nil
		},
	}
	written, rest, err := expandBareISOFiles(context.Background(), deps, files, "vdir", "movie", "", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(written) != 0 {
		t.Errorf("written = %v, want [] (no metadata should be written when expansion is gated off)", written)
	}
	if len(rest) != 1 || rest[0].Filename != "movie.iso" {
		t.Errorf("rest = %+v, want the original .iso pushed back for normal dispatch", rest)
	}
}

// TestExpandBareISOFiles_PropagatesSourceNzbPathAndReleaseDate asserts the
// orchestrator threads sourceNzbPath and releaseDate through to the
// FileMetadata produced via archive.NewFileMetadataFromContent. Without
// this, downstream consumers (history, repair, etc.) lose the link back
// to the originating NZB post.
func TestExpandBareISOFiles_PropagatesSourceNzbPathAndReleaseDate(t *testing.T) {
	files := []parser.ParsedFile{{Filename: "movie.iso", Size: 1000}}

	const wantSourceNzbPath = "/incoming/Movie.1080p.BluRay.nzb"
	const wantReleaseDate int64 = 1_234_567_890

	var capturedMeta *metapb.FileMetadata
	deps := expandBareISODeps{
		enabled: true,
		expand: func(ctx context.Context, _ bool, _ []archive.Content) ([]archive.Content, error) {
			return []archive.Content{{
				Filename: "MOVIE.m2ts",
				Size:     900,
				NestedSources: []archive.NestedSource{
					{InnerOffset: 0, InnerLength: 900},
				},
			}}, nil
		},
		writeMetadata: func(_ string, meta *metapb.FileMetadata) error {
			capturedMeta = meta
			return nil
		},
	}

	written, _, err := expandBareISOFiles(
		context.Background(), deps, files, "vdir", "movie",
		wantSourceNzbPath, wantReleaseDate,
	)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("written = %v, want 1 entry", written)
	}
	if capturedMeta == nil {
		t.Fatal("writeMetadata was never invoked")
	}
	if capturedMeta.SourceNzbPath != wantSourceNzbPath {
		t.Errorf("SourceNzbPath = %q, want %q", capturedMeta.SourceNzbPath, wantSourceNzbPath)
	}
	if capturedMeta.ReleaseDate != wantReleaseDate {
		t.Errorf("ReleaseDate = %d, want %d", capturedMeta.ReleaseDate, wantReleaseDate)
	}
}

// TestExpandBareISOFiles_MultipleISOs_WritesAllInParallel verifies that when
// multiple ISOs expand successfully, all their metadata is written and the
// race detector finds no data races.
func TestExpandBareISOFiles_MultipleISOs_WritesAllInParallel(t *testing.T) {
	files := []parser.ParsedFile{
		{Filename: "DISC_1.iso", Size: 1000},
		{Filename: "DISC_2.iso", Size: 2000},
		{Filename: "DISC_3.iso", Size: 3000},
	}

	var writtenMu sync.Mutex
	var writtenPaths []string

	deps := expandBareISODeps{
		enabled: true,
		expand: func(_ context.Context, _ bool, in []archive.Content) ([]archive.Content, error) {
			out := make([]archive.Content, len(in))
			for i, c := range in {
				out[i] = archive.Content{
					Filename: strings.TrimSuffix(c.Filename, ".iso") + ".m2ts",
					Size:     c.Size,
					NestedSources: []archive.NestedSource{
						{InnerOffset: 0, InnerLength: c.Size},
					},
				}
			}
			return out, nil
		},
		writeMetadata: func(virtualPath string, _ *metapb.FileMetadata) error {
			writtenMu.Lock()
			writtenPaths = append(writtenPaths, virtualPath)
			writtenMu.Unlock()
			return nil
		},
	}

	written, rest, err := expandBareISOFiles(context.Background(), deps, files, "vdir", "movie", "", 0)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(written) != 3 {
		t.Errorf("written = %v, want 3 paths", written)
	}
	if len(rest) != 0 {
		t.Errorf("rest = %v, want empty (all ISOs expanded)", rest)
	}

	sort.Strings(writtenPaths)
	want := []string{"vdir/DISC_1.m2ts", "vdir/DISC_2.m2ts", "vdir/DISC_3.m2ts"}
	for i, w := range want {
		if i >= len(writtenPaths) || writtenPaths[i] != w {
			t.Errorf("writtenPaths[%d] = %q, want %q", i, writtenPaths[i], w)
		}
	}
}
