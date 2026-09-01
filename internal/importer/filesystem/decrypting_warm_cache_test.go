package filesystem

import (
	"context"
	"io"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
)

// TestDecryptingWarmCacheReadsPastPrefix pins the invariant that the warm
// first-segment shortcut never truncates a read. FirstSegmentBytes covers only
// the leading bytes of the volume, so a read spanning the whole file must serve
// the prefix from memory and fetch the remainder from the wire — returning the
// complete byte range, not a short EOF at the prefix boundary.
func TestDecryptingWarmCacheReadsPastPrefix(t *testing.T) {
	const prefixLen = 16
	const tailLen = 24
	const total = prefixLen + tailLen

	full := make([]byte, total)
	for i := range full {
		full[i] = byte('A' + (i % 26))
	}
	warm := full[:prefixLen]

	fpc := fakepool.New()
	fpc.SetBehavior("<seg0>", fakepool.SegmentBehavior{Bytes: full})
	mgr := &fsFakePoolManager{client: fpc}

	entry := DecryptingFileEntry{
		Filename:      "inner.part01.rar",
		DecryptedSize: total,
		Segments: []*metapb.SegmentData{
			{Id: "<seg0>", StartOffset: 0, EndOffset: total - 1, SegmentSize: total},
		},
		FirstSegmentBytes: warm,
	}

	dfs := NewDecryptingFileSystem(context.Background(), mgr, []DecryptingFileEntry{entry}, 1, time.Minute, nil)
	f, err := dfs.Open("inner.part01.rar")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	got, err := io.ReadAll(f)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != total {
		t.Fatalf("ReadAll returned %d bytes, want %d (warm prefix truncated the stream)", len(got), total)
	}
	if string(got) != string(full) {
		t.Fatalf("content mismatch:\n got %q\nwant %q", got, full)
	}
}

// TestDecryptingWarmCacheServesPrefixWithoutNetwork verifies the optimization
// still pays off: a read confined to the warm prefix issues zero wire calls.
func TestDecryptingWarmCacheServesPrefixWithoutNetwork(t *testing.T) {
	const total = 40
	full := make([]byte, total)
	for i := range full {
		full[i] = byte('a' + (i % 26))
	}

	fpc := fakepool.New()
	mgr := &fsFakePoolManager{client: fpc}

	entry := DecryptingFileEntry{
		Filename:      "inner.part01.rar",
		DecryptedSize: total,
		Segments: []*metapb.SegmentData{
			{Id: "<seg0>", StartOffset: 0, EndOffset: total - 1, SegmentSize: total},
		},
		FirstSegmentBytes: full,
	}

	dfs := NewDecryptingFileSystem(context.Background(), mgr, []DecryptingFileEntry{entry}, 1, time.Minute, nil)
	f, err := dfs.Open("inner.part01.rar")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer f.Close()

	buf := make([]byte, 10)
	if _, err := io.ReadFull(f, buf); err != nil {
		t.Fatalf("ReadFull: %v", err)
	}
	if string(buf) != string(full[:10]) {
		t.Fatalf("prefix bytes mismatch:\n got %q\nwant %q", buf, full[:10])
	}
	if got := fpc.TotalCalls(); got != 0 {
		t.Errorf("warm prefix read issued %d wire calls, want 0", got)
	}
}
