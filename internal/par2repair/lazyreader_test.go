package par2repair

import (
	"context"
	"io"
	"testing"
)

// A vanished article (430 mid-parse) must cost exactly ONE wire round trip:
// the reader zero-fills it and remembers the verdict. Without that, resync
// scans through the zeroed region in small chunks and re-fetches the same
// dead article once per chunk — a fully-purged 440-article volume then costs
// hours instead of minutes (observed live against a takedown-in-progress
// release on newshosting).
func TestLazyFileReaderFetchesVanishedArticleOnce(t *testing.T) {
	inner := &fakeFetcher{articles: map[string][]byte{
		"live-0@test": make([]byte, 4096),
		// gone-1@test absent: every fetch answers 430.
		"live-2@test": make([]byte, 4096),
	}}
	fetch := &countingFetcher{inner: inner}
	file := SetFile{
		Length: 3 * 4096,
		Articles: []Article{
			{MessageID: "live-0@test", Size: 4096},
			{MessageID: "gone-1@test", Size: 4096},
			{MessageID: "live-2@test", Size: 4096},
		},
	}

	r := newLazyFileReader(context.Background(), fetch, file, newArticleCache(64))
	buf := make([]byte, 512) // small chunks, like resync's scan
	total := 0
	for {
		n, err := r.Read(buf)
		total += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
	}
	if total != 3*4096 {
		t.Fatalf("read %d bytes, want %d", total, 3*4096)
	}
	if got := fetch.calls["gone-1@test"]; got != 1 {
		t.Fatalf("vanished article fetched %d times, want 1 (verdict must be remembered)", got)
	}
}
