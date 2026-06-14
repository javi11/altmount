package rar

import (
	"context"
	"fmt"
	"log/slog"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser"
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
