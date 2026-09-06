package filesystem

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
)

// TestUsenetFileReadAtReusesCachedSegment pins the core import-scoped cache
// contract: UsenetFile.ReadAt builds a brand-new UsenetReader on every call
// (see createUsenetReader), so without a shared segment cache, two ReadAt
// calls that land on the same segment issue two separate wire fetches for
// the same message-ID. Wiring an ImportSegmentCache through the filesystem
// must collapse that to a single fetch.
func TestUsenetFileReadAtReusesCachedSegment(t *testing.T) {
	const segID = "<seg0>"
	const size = 30
	full := make([]byte, size)
	for i := range full {
		full[i] = byte('A' + (i % 26))
	}

	fpc := fakepool.New()
	fpc.SetBehavior(segID, fakepool.SegmentBehavior{Bytes: full})
	mgr := &fsFakePoolManager{client: fpc}

	file := parser.ParsedFile{
		Filename: "movie.part01.rar",
		Size:     size,
		Segments: []*metapb.SegmentData{
			{Id: segID, StartOffset: 0, EndOffset: size - 1, SegmentSize: size},
		},
	}

	store := NewImportSegmentCache(0)
	ufs := NewUsenetFileSystem(context.Background(), mgr, []parser.ParsedFile{file}, 1, nil, time.Minute, store)

	f, err := ufs.Open(file.Filename)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })

	ra, ok := f.(interface {
		ReadAt(p []byte, off int64) (int, error)
	})
	if !ok {
		t.Fatalf("file does not implement io.ReaderAt")
	}

	buf1 := make([]byte, size)
	if _, err := ra.ReadAt(buf1, 0); err != nil {
		t.Fatalf("first ReadAt: %v", err)
	}
	if string(buf1) != string(full) {
		t.Fatalf("first ReadAt bytes mismatch:\n got %q\nwant %q", buf1, full)
	}

	buf2 := make([]byte, size)
	if _, err := ra.ReadAt(buf2, 0); err != nil {
		t.Fatalf("second ReadAt: %v", err)
	}
	if string(buf2) != string(full) {
		t.Fatalf("second ReadAt bytes mismatch:\n got %q\nwant %q", buf2, full)
	}

	if got := fpc.PerMessageCalls(segID); got != 1 {
		t.Errorf("PerMessageCalls(%q) = %d, want 1 (second ReadAt should hit the import segment cache, not the wire)", segID, got)
	}
}
