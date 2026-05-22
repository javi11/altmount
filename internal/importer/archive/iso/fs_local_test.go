package iso

import (
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestLocalISO_DiscoverBigFiles is a manual integration test: it walks a
// real Blu-ray ISO from local disk and dumps a size-sorted summary. Skipped
// unless ALTMOUNT_LOCAL_ISO is set, so CI stays unaffected.
//
// Set ALTMOUNT_LOCAL_ISO=/abs/path/to.iso to run, e.g.:
//
//	ALTMOUNT_LOCAL_ISO=/Volumes/.../DISC_1.iso go test \
//	  ./internal/importer/archive/iso/... -run TestLocalISO -v
func TestLocalISO_DiscoverBigFiles(t *testing.T) {
	path := os.Getenv("ALTMOUNT_LOCAL_ISO")
	if path == "" {
		t.Skip("ALTMOUNT_LOCAL_ISO not set")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()

	stat, _ := f.Stat()
	t.Logf("ISO: %s  size=%d (%.2f GiB)", path, stat.Size(), float64(stat.Size())/(1<<30))

	entries, err := ListISOFiles(f)
	if err != nil {
		t.Fatalf("ListISOFiles: %v", err)
	}

	var sum int64
	for _, e := range entries {
		sum += int64(e.size)
	}
	t.Logf("listed_files=%d  listed_sum=%d (%.2f GiB)  coverage=%.1f%%",
		len(entries), sum, float64(sum)/(1<<30), 100*float64(sum)/float64(stat.Size()))

	// Top 25 by size — should match `ls -laS BDMV/STREAM/` if walker is sane.
	sort.Slice(entries, func(i, j int) bool { return entries[i].size > entries[j].size })
	t.Logf("top 25 by size:")
	for i, e := range entries {
		if i >= 25 {
			break
		}
		t.Logf("  %s  size=%d (%.2f MiB)  lba=%d", e.path, e.size, float64(e.size)/(1<<20), e.lba)
	}

	// Sanity sentinels for the Avatar disc 1 main-feature clips. Each one
	// is >1 GiB on disc, so if any are absent the walker dropped them.
	want := []string{"BDMV/STREAM/00016.m2ts", "BDMV/STREAM/00022.m2ts", "BDMV/STREAM/00028.m2ts"}
	have := make(map[string]uint64, len(entries))
	for _, e := range entries {
		have[e.path] = e.size
	}
	for _, w := range want {
		size, ok := have[w]
		if !ok {
			t.Errorf("missing %s — walker dropped this file", w)
			continue
		}
		if size < 1<<30 {
			t.Errorf("%s reported size=%d (%.2f MiB), want >1 GiB",
				w, size, float64(size)/(1<<20))
		}
	}

	if t.Failed() {
		fmt.Println(">>> walker is dropping big files; this is the bug")
	}
}
