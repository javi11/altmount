package nzbgap

import (
	"os"
	"strings"
	"testing"

	"github.com/javi11/nzbparser"
)

// Local-only sanity check against the real-world failed NZB that motivated
// gap support. Skipped when the file isn't present (CI).
func TestFillRealWorldGappyNzb(t *testing.T) {
	const path = "/Users/javi/mio/altmount/example/.nzbs/failed/It (1).nzb"
	f, err := os.Open(path)
	if err != nil {
		t.Skipf("local fixture not available: %v", err)
	}
	defer f.Close()
	n, err := nzbparser.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	gaps := Fill(n)
	total := 0
	for _, ids := range gaps {
		total += len(ids)
	}
	t.Logf("files_with_gaps=%d total_gap_segments=%d", len(gaps), total)
	if len(gaps) < 40 || total < 70 {
		t.Fatalf("expected ~44 gap files / ~80 gap segments, got %d/%d", len(gaps), total)
	}
	for name, ids := range gaps {
		if strings.HasSuffix(name, ".r43") {
			if len(ids) != 2 {
				t.Fatalf("r43 gaps = %v, want 2 (numbers 6 and 34)", ids)
			}
		}
	}
}
