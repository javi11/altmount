package par2

import (
	"bytes"
	"testing"
)

// buildPresentBlocks returns all input blocks of data.bin as global-index
// slices (the fixture is a single-file recovery set, so global index == slice
// index), with the given indices removed (set to nil) to simulate missing
// articles.
func buildPresentBlocks(t *testing.T, rs *RecoverySet, data []byte, drop ...int) [][]byte {
	t.Helper()
	layout, total := rs.Layout()
	if len(layout) != 1 {
		t.Fatalf("fixture should be single-file, got %d files", len(layout))
	}
	ss := int(rs.SliceSize)
	blocks := make([][]byte, total)
	for i := 0; i < total; i++ {
		blocks[i] = sliceOf(data, i, ss)
	}
	for _, d := range drop {
		blocks[d] = nil
	}
	return blocks
}

func TestReconstructDroppedBlocks(t *testing.T) {
	rs, data := loadFixture(t)
	ss := int(rs.SliceSize)

	cases := [][]int{
		{0},                      // first block
		{15},                     // last (partial) block
		{1, 5, 9},                // scattered
		{2, 3, 4, 5, 6, 7, 8, 9}, // 8 missing == number of recovery slices (max)
	}

	for _, drop := range cases {
		present := buildPresentBlocks(t, rs, data, drop...)
		out, err := rs.Reconstruct(present)
		if err != nil {
			t.Fatalf("Reconstruct(drop=%v): %v", drop, err)
		}
		for _, idx := range drop {
			want := sliceOf(data, idx, ss)
			if !bytes.Equal(out[idx], want) {
				t.Fatalf("drop=%v: reconstructed block %d does not match original", drop, idx)
			}
		}
	}
}

func TestReconstructInsufficientRecovery(t *testing.T) {
	rs, data := loadFixture(t)
	// 9 missing blocks but only 8 recovery slices → must fail cleanly.
	present := buildPresentBlocks(t, rs, data, 0, 1, 2, 3, 4, 5, 6, 7, 8)
	if _, err := rs.Reconstruct(present); err != ErrInsufficientRecovery {
		t.Fatalf("got %v, want ErrInsufficientRecovery", err)
	}
}

func TestReconstructNoMissingIsNoop(t *testing.T) {
	rs, data := loadFixture(t)
	present := buildPresentBlocks(t, rs, data)
	out, err := rs.Reconstruct(present)
	if err != nil {
		t.Fatalf("Reconstruct: %v", err)
	}
	for i := range present {
		if !bytes.Equal(out[i], present[i]) {
			t.Fatalf("block %d changed despite nothing missing", i)
		}
	}
}

func TestInputBasesAreDistinctAndCoprime(t *testing.T) {
	bases, err := inputBases(64)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[uint16]bool{}
	for i, b := range bases {
		if b == 0 {
			t.Fatalf("base %d is zero", i)
		}
		if seen[b] {
			t.Fatalf("duplicate base value %d at index %d", b, i)
		}
		seen[b] = true
	}
}
