package multifile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/progress"
)

// recordingBroadcaster captures progress updates for assertions.
type recordingBroadcaster struct {
	mu      sync.Mutex
	updates int
	maxPct  int
	stages  map[string]struct{}
}

func newRecordingBroadcaster() *recordingBroadcaster {
	return &recordingBroadcaster{stages: make(map[string]struct{})}
}

func (b *recordingBroadcaster) UpdateProgress(_ int, percentage int) {
	b.UpdateProgressWithStage(0, percentage, "")
}

func (b *recordingBroadcaster) UpdateProgressWithStage(_ int, percentage int, stage string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.updates++
	if percentage > b.maxPct {
		b.maxPct = percentage
	}
	b.stages[stage] = struct{}{}
}

func TestProcessRegularFilesSkipsFileWithSizeMismatch(t *testing.T) {
	ctx := context.Background()
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)

	files := []parser.ParsedFile{
		parsedTestFile("Show.S01E01.mkv", "healthy-segment"),
		parsedTestFileSizeMismatch("Show.S01E02.mkv", "bad-segment"),
	}

	writtenPaths, err := ProcessRegularFiles(
		ctx,
		"tv/Show/Season 01",
		files,
		nil,
		"Show.S01.nzb",
		svc,
		[]string{".mkv"},
		true,
		nil,
	)
	if err != nil {
		t.Fatalf("ProcessRegularFiles returned error: %v", err)
	}

	if len(writtenPaths) != 1 || writtenPaths[0] != "tv/Show/Season 01/Show.S01E01.mkv" {
		t.Fatalf("writtenPaths = %v, want only healthy episode", writtenPaths)
	}
	if !metadataExists(t, metaRoot, "tv/Show/Season 01/Show.S01E01.mkv") {
		t.Fatal("healthy episode metadata was not written")
	}
	if metadataExists(t, metaRoot, "tv/Show/Season 01/Show.S01E02.mkv") {
		t.Fatal("failed episode metadata was written")
	}
}

func TestProcessRegularFilesFailsWhenAllFilesFailValidation(t *testing.T) {
	ctx := context.Background()
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)

	files := []parser.ParsedFile{
		parsedTestFileSizeMismatch("Show.S01E01.mkv", "bad-1"),
		parsedTestFileSizeMismatch("Show.S01E02.mkv", "bad-2"),
	}

	writtenPaths, err := ProcessRegularFiles(
		ctx,
		"tv/Show/Season 01",
		files,
		nil,
		"Show.S01.nzb",
		svc,
		[]string{".mkv"},
		true,
		nil,
	)
	if err == nil {
		t.Fatal("ProcessRegularFiles returned nil error, want all-files-failed error")
	}
	if !errors.Is(err, ErrNoFilesProcessed) {
		t.Fatalf("ProcessRegularFiles error = %v, want ErrNoFilesProcessed", err)
	}
	if len(writtenPaths) != 0 {
		t.Fatalf("writtenPaths = %v, want none", writtenPaths)
	}
}

// TestProcessRegularFilesManyCollidingNames simulates an obfuscated Blu-ray
// BDMV release where every file inherits the same release name (all colliding
// to one virtual path). Each colliding file must receive a distinct _N suffix
// and all metadata must be written. This guards the reservation fast-path:
// ReserveUniqueVirtualPath must stay correct when hundreds of files collide.
func TestProcessRegularFilesManyCollidingNames(t *testing.T) {
	ctx := context.Background()
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)

	const n = 200
	files := make([]parser.ParsedFile, n)
	for i := range files {
		// All files share the same name, forcing collisions.
		files[i] = parsedTestFile("Movie.BluRay.clpi", "seg")
	}

	bc := newRecordingBroadcaster()
	tracker := progress.NewTracker(bc, 1, 30, 95).WithStage("Writing metadata")

	writtenPaths, err := ProcessRegularFiles(
		ctx,
		"movies/Movie.BluRay",
		files,
		nil,
		"Movie.BluRay.nzb",
		svc,
		[]string{".clpi"},
		true,
		tracker,
	)
	if err != nil {
		t.Fatalf("ProcessRegularFiles returned error: %v", err)
	}

	if len(writtenPaths) != n {
		t.Fatalf("writtenPaths = %d, want %d distinct paths", len(writtenPaths), n)
	}

	// Progress must advance as files complete (not frozen) and use the correct stage.
	if bc.updates == 0 {
		t.Fatal("expected progress updates during the write loop, got none")
	}
	if bc.maxPct < 95 {
		t.Fatalf("progress reached only %d%%, want it to climb to the 95%% band top", bc.maxPct)
	}
	if _, ok := bc.stages["Writing metadata"]; !ok {
		t.Fatalf("expected 'Writing metadata' stage, got stages %v", bc.stages)
	}

	// Every written path must be unique and exist on disk.
	seen := make(map[string]struct{}, n)
	for _, p := range writtenPaths {
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate written path: %s", p)
		}
		seen[p] = struct{}{}
		if !metadataExists(t, metaRoot, p) {
			t.Fatalf("metadata not written to disk for %s", p)
		}
	}
}

// parsedTestFile creates a file where declared size matches segment bytes.
func parsedTestFile(filename, segmentID string) parser.ParsedFile {
	return parser.ParsedFile{
		Filename: filename,
		Size:     100,
		Segments: []*metapb.SegmentData{
			{Id: segmentID, StartOffset: 0, EndOffset: 99},
		},
		ReleaseDate: time.Unix(1, 0),
	}
}

// parsedTestFileSizeMismatch creates a file where declared size does NOT match segment bytes.
func parsedTestFileSizeMismatch(filename, segmentID string) parser.ParsedFile {
	return parser.ParsedFile{
		Filename: filename,
		Size:     999, // declared larger than the 100-byte segment
		Segments: []*metapb.SegmentData{
			{Id: segmentID, StartOffset: 0, EndOffset: 99},
		},
		ReleaseDate: time.Unix(1, 0),
	}
}

func metadataExists(t *testing.T, metaRoot, virtualPath string) bool {
	t.Helper()
	_, err := os.Stat(filepath.Join(metaRoot, virtualPath+".meta"))
	return err == nil
}
