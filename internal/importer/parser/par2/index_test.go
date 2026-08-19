package par2_test

import (
	"bytes"
	"io"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	"github.com/javi11/altmount/internal/testsupport/par2gen"
)

func TestParseIndexRoundTrip(t *testing.T) {
	dataA := bytes.Repeat([]byte("abcdefgh"), 1000) // 8000 B -> 8 slices of 1024
	dataB := bytes.Repeat([]byte("01234567"), 700)  // 5600 B -> 6 slices of 1024
	set := par2gen.BuildFull(1024, []par2gen.FileEntry{
		{Name: "a.rar", Content: dataA},
		{Name: "b.rar", Content: dataB},
	}, 4)

	streams := []io.Reader{bytes.NewReader(set.Index)}
	for _, v := range set.Volumes {
		streams = append(streams, bytes.NewReader(v))
	}
	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatal(err)
	}
	if idx.SliceSize != 1024 {
		t.Fatalf("slice size = %d", idx.SliceSize)
	}
	if len(idx.RecoveryIDs) != 2 {
		t.Fatalf("recovery set size = %d", len(idx.RecoveryIDs))
	}
	if bytes.Compare(idx.RecoveryIDs[0][:], idx.RecoveryIDs[1][:]) >= 0 {
		t.Fatal("RecoveryIDs not sorted ascending")
	}
	total := 0
	for _, id := range idx.RecoveryIDs {
		fd, ok := idx.Files[id]
		if !ok {
			t.Fatalf("missing FileDesc for recovery-set member %x", id)
		}
		checks, ok := idx.SliceChecks[id]
		if !ok {
			t.Fatalf("missing IFSC for recovery-set member %x", id)
		}
		wantSlices := int((fd.Length + 1023) / 1024)
		if len(checks) != wantSlices {
			t.Fatalf("file %q: %d slice checks, want %d", fd.Name, len(checks), wantSlices)
		}
		total += len(checks)
	}
	if total != 14 {
		t.Fatalf("total slices = %d, want 14", total)
	}
	if len(idx.Recovery) != 4 {
		t.Fatalf("recovery slices = %d, want 4", len(idx.Recovery))
	}
	seen := map[uint32]bool{}
	for _, r := range idx.Recovery {
		if seen[r.Exponent] {
			t.Fatalf("duplicate exponent %d", r.Exponent)
		}
		seen[r.Exponent] = true
		if r.BodyOffset <= 0 {
			t.Fatalf("recovery slice exponent %d has BodyOffset %d", r.Exponent, r.BodyOffset)
		}
		if r.FileIndex <= 0 || r.FileIndex >= len(streams) {
			t.Fatalf("recovery slice exponent %d has FileIndex %d", r.Exponent, r.FileIndex)
		}
	}
}

// The recovery payload located by a RecoverySliceRef must be exactly the bytes
// the generator produced: re-read the payload via the recorded offset and
// compare against an independently computed recovery slice in the solver test
// (Task 2). Here we only verify offsets point at plausible payloads.
func TestParseIndexRecoveryOffsets(t *testing.T) {
	content := bytes.Repeat([]byte{0xAB}, 4096)
	set := par2gen.BuildFull(512, []par2gen.FileEntry{{Name: "x.bin", Content: content}}, 2)

	streams := []io.Reader{bytes.NewReader(set.Index)}
	for _, v := range set.Volumes {
		streams = append(streams, bytes.NewReader(v))
	}
	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatal(err)
	}
	all := append([][]byte{set.Index}, set.Volumes...)
	for _, r := range idx.Recovery {
		src := all[r.FileIndex]
		if int64(len(src)) < r.BodyOffset+int64(idx.SliceSize) {
			t.Fatalf("payload for exponent %d out of bounds", r.Exponent)
		}
	}
}

func TestParseIndexErrors(t *testing.T) {
	// no Main packet: index containing only FileDescs
	legacy := par2gen.Build(par2gen.FileEntry{Name: "a", Content: []byte("hello")})
	if _, err := par2.ParseIndex([]io.Reader{bytes.NewReader(legacy)}); err == nil {
		t.Fatal("want error for missing Main packet")
	}
}
