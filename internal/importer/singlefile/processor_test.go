package singlefile

import (
	"context"
	"github.com/kipsilabs/altmount/internal/importer/parser"
	"github.com/kipsilabs/altmount/internal/metadata"
	pb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"testing"
	"time"
)

func TestOriginalFilenameSurvivesDisambiguation(t *testing.T) {
	svc := metadata.NewMetadataService(t.TempDir())
	f := parser.ParsedFile{Filename: "movie.mkv", Size: 3, ReleaseDate: time.Now(), Segments: []*pb.SegmentData{{Id: "id", SegmentSize: 3, EndOffset: 2}}}
	for range 2 {
		_, p, e := ProcessSingleFile(context.Background(), "movies", f, nil, "source.nzb", svc, []string{".mkv"}, false, nil, "")
		if e != nil {
			t.Fatal(e)
		}
		m, e := svc.ReadFileMetadata(p)
		if e != nil {
			t.Fatal(e)
		}
		if m.GetOriginalFilename() != "movie.mkv" {
			t.Fatalf("original filename lost at %s: %q", p, m.GetOriginalFilename())
		}
	}
}

func TestOriginalFilenameSurvivesImportRename(t *testing.T) {
	svc := metadata.NewMetadataService(t.TempDir())
	file := parser.ParsedFile{Filename: "display.mkv", OriginalFilename: "source.mkv", Size: 3, ReleaseDate: time.Now(), Segments: []*pb.SegmentData{{Id: "id", SegmentSize: 3, EndOffset: 2}}}
	_, p, err := ProcessSingleFile(context.Background(), "movies", file, nil, "source.nzb", svc, []string{".mkv"}, false, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	m, err := svc.ReadFileMetadata(p)
	if err != nil {
		t.Fatal(err)
	}
	if m.OriginalFilename != "source.mkv" {
		t.Fatalf("got original %q", m.OriginalFilename)
	}
}
