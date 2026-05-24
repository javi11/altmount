package importer

import (
	"testing"

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
