package par2

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func loadBigFixture(t *testing.T) (*RecoverySet, []byte) {
	t.Helper()
	var all []byte
	for _, name := range []string{"big.par2", "big.vol00+40.par2"} {
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			t.Skipf("big fixture missing (%s): %v", name, err)
		}
		all = append(all, b...)
	}
	rs, err := Parse(all)
	if err != nil {
		t.Fatalf("Parse big: %v", err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", "big.bin"))
	if err != nil {
		t.Fatalf("read big.bin: %v", err)
	}
	return rs, data
}

// TestReconstructBigFixture exercises the engine on 123 input blocks with 40
// recovery blocks, dropping 40 scattered blocks (the maximum the recovery set
// can repair). This stresses the base-constant generation and the GF solver at
// a realistic block count.
func TestReconstructBigFixture(t *testing.T) {
	rs, data := loadBigFixture(t)
	ss := int(rs.SliceSize)
	_, total := rs.Layout()

	present := make([][]byte, total)
	for i := 0; i < total; i++ {
		present[i] = sliceOf(data, i, ss)
	}
	// Drop 40 blocks spread across the file (every 3rd, capped at 40).
	var dropped []int
	for i := 0; i < total && len(dropped) < 40; i += 3 {
		present[i] = nil
		dropped = append(dropped, i)
	}

	out, err := rs.Reconstruct(present)
	if err != nil {
		t.Fatalf("Reconstruct big (dropped %d): %v", len(dropped), err)
	}
	for _, idx := range dropped {
		if !bytes.Equal(out[idx], sliceOf(data, idx, ss)) {
			t.Fatalf("big: block %d reconstructed incorrectly", idx)
		}
	}
}
