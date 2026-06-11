package multifile

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	"github.com/javi11/altmount/internal/metadata"
)

// TestProcessRegularFilesCollisionScaling guards against O(N^2) regressions in
// unique-path reservation. With the PathReserver high-water mark, 4000 files
// that all collide to one name must be assigned distinct suffixes in roughly
// linear time. Before the fix this took ~9s (quadratic); the generous 4s bound
// fails loudly if quadratic probing is reintroduced while tolerating slow CI.
func TestProcessRegularFilesCollisionScaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in -short mode")
	}

	const n = 4000
	metaRoot := t.TempDir()
	svc := metadata.NewMetadataService(metaRoot)
	files := make([]parser.ParsedFile, n)
	for i := range files {
		files[i] = parsedTestFile("Movie.BluRay.clpi", "seg")
	}

	start := time.Now()
	written, err := ProcessRegularFiles(
		context.Background(),
		"movies/Movie.BluRay",
		files, nil, "Movie.BluRay.nzb",
		svc, []string{".clpi"}, true, nil,
	)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("ProcessRegularFiles returned error: %v", err)
	}
	if len(written) != n {
		t.Fatalf("written = %d, want %d distinct paths", len(written), n)
	}
	if elapsed > 4*time.Second {
		t.Fatalf("reservation took %v for %d colliding files; expected near-linear time (quadratic regression?)", elapsed, n)
	}

	// All assigned paths must be unique.
	seen := make(map[string]struct{}, n)
	for _, p := range written {
		if _, dup := seen[p]; dup {
			t.Fatalf("duplicate written path: %s", p)
		}
		seen[p] = struct{}{}
	}
}
