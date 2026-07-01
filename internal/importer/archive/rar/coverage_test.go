package rar

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/rardecode/v2"
)

func makeVolumes(base string, n int, size int64) []parser.ParsedFile {
	vols := make([]parser.ParsedFile, n)
	for i := 0; i < n; i++ {
		vols[i] = parser.ParsedFile{Filename: fmt.Sprintf("%s.part%d.rar", base, i+1), Size: size}
	}
	return vols
}

func makeParts(base string, n int, packed int64) []rardecode.FilePartInfo {
	parts := make([]rardecode.FilePartInfo, n)
	for i := 0; i < n; i++ {
		parts[i] = rardecode.FilePartInfo{Path: fmt.Sprintf("%s.part%d.rar", base, i+1), PackedSize: packed}
	}
	return parts
}

func TestCheckVolumeCoverage(t *testing.T) {
	const volSize = int64(105_216_000)
	log := slog.Default()

	t.Run("truncated set fails", func(t *testing.T) {
		// 259 volumes supplied, but only 9 followed (the reported bug).
		volumes := makeVolumes("Movie", 259, volSize)
		agg := []rardecode.ArchiveFileInfo{{Parts: makeParts("Movie", 9, volSize)}}
		if err := checkVolumeCoverage(context.Background(), log, agg, volumes, "Movie.part1.rar"); err == nil {
			t.Fatal("expected truncation error, got nil")
		}
	})

	t.Run("complete set passes", func(t *testing.T) {
		volumes := makeVolumes("Movie", 259, volSize)
		// Followed payload slightly below volume bytes (per-volume header overhead).
		agg := []rardecode.ArchiveFileInfo{{Parts: makeParts("Movie", 259, volSize-2048)}}
		if err := checkVolumeCoverage(context.Background(), log, agg, volumes, "Movie.part1.rar"); err != nil {
			t.Fatalf("expected nil for complete set, got %v", err)
		}
	})

	t.Run("unknown sizes are not judged", func(t *testing.T) {
		volumes := makeVolumes("Movie", 10, 0) // sizes unknown
		agg := []rardecode.ArchiveFileInfo{{Parts: makeParts("Movie", 1, 0)}}
		if err := checkVolumeCoverage(context.Background(), log, agg, volumes, "Movie.part1.rar"); err != nil {
			t.Fatalf("expected nil when sizes unknown, got %v", err)
		}
	})
}

func TestCheckAnalyzedContentCoverage(t *testing.T) {
	log := slog.Default()
	cseg := func(id string, size int64) *metapb.SegmentData {
		return &metapb.SegmentData{Id: id, StartOffset: 0, EndOffset: size - 1, SegmentSize: size}
	}

	t.Run("declared size not backed by segments fails (shattered obfuscated set)", func(t *testing.T) {
		// rardecode reported the inner .mkv's full 14.6 GB from the header, but only one
		// ~150 MB volume was mapped because the set stayed shattered into single-file groups.
		contents := []Content{{
			Filename: "finding.you.mkv",
			Size:     14_671_485_655,
			Segments: []*metapb.SegmentData{cseg("a@x", 149_999_866)},
		}}
		if err := checkAnalyzedContentCoverage(context.Background(), log, contents); err == nil {
			t.Fatal("expected coverage error for under-backed file, got nil")
		}
	})

	t.Run("fully backed stored file passes", func(t *testing.T) {
		contents := []Content{{
			Filename: "movie.mkv",
			Size:     300,
			Segments: []*metapb.SegmentData{cseg("a@x", 100), cseg("b@x", 100), cseg("c@x", 100)},
		}}
		if err := checkAnalyzedContentCoverage(context.Background(), log, contents); err != nil {
			t.Fatalf("expected nil for fully backed file, got %v", err)
		}
	})

	t.Run("nested sources judged by inner length", func(t *testing.T) {
		contents := []Content{{
			Filename:      "inner.mkv",
			Size:          200,
			NestedSources: []NestedSource{{InnerLength: 120}, {InnerLength: 80}},
		}}
		if err := checkAnalyzedContentCoverage(context.Background(), log, contents); err != nil {
			t.Fatalf("expected nil for fully covered nested file, got %v", err)
		}
	})

	t.Run("directories and zero-size entries skipped", func(t *testing.T) {
		contents := []Content{
			{Filename: "dir", IsDirectory: true, Size: 999},
			{Filename: "empty", Size: 0},
		}
		if err := checkAnalyzedContentCoverage(context.Background(), log, contents); err != nil {
			t.Fatalf("expected nil, got %v", err)
		}
	})
}
