package par2repair

import (
	"bytes"
	"math/rand"
	"testing"

	"github.com/akalin/gopar/gf2p16"
)

// buildRecovery computes recovery slices the spec way (direct sum over all
// slices), independently of Solver's incremental fold.
func buildRecovery(slices [][]byte, exps []uint32, sliceSize int) [][]byte {
	out := make([][]byte, len(exps))
	for i, e := range exps {
		acc := make([]byte, sliceSize)
		for j, sl := range slices {
			gf2p16.MulAndAddByteSliceLE(VandermondeBase(j).Pow(e), sl, acc)
		}
		out[i] = acc
	}
	return out
}

func TestSolverRoundTrip(t *testing.T) {
	const sliceSize, n = 512, 20
	rng := rand.New(rand.NewSource(42))
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = make([]byte, sliceSize)
		rng.Read(slices[j])
	}
	missing := []int{3, 7, 19}
	exps := []uint32{0, 1, 2}
	rec := buildRecovery(slices, exps, sliceSize)

	s, err := NewSolver(missing, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rec {
		s.AddRecovery(i, r)
	}
	for j, sl := range slices {
		if j == 3 || j == 7 || j == 19 {
			continue
		}
		s.FoldPresent(j, sl)
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	for i, m := range missing {
		if !bytes.Equal(got[i], slices[m]) {
			t.Fatalf("slice %d not recovered", m)
		}
	}
}

func TestSolverSingleMissingHighExponent(t *testing.T) {
	const sliceSize, n = 256, 5
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = bytes.Repeat([]byte{byte(j + 1)}, sliceSize)
	}
	// Use a non-contiguous exponent, as when earlier volumes are dead.
	exps := []uint32{7}
	rec := buildRecovery(slices, exps, sliceSize)

	s, err := NewSolver([]int{2}, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	s.AddRecovery(0, rec[0])
	for j, sl := range slices {
		if j != 2 {
			s.FoldPresent(j, sl)
		}
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0], slices[2]) {
		t.Fatal("slice 2 not recovered")
	}
}

func TestSolverKMismatch(t *testing.T) {
	if _, err := NewSolver([]int{1, 2}, []uint32{0}, 512); err == nil {
		t.Fatal("want error: need one recovery slice per missing slice")
	}
	if _, err := NewSolver(nil, nil, 512); err == nil {
		t.Fatal("want error: nothing to solve")
	}
	if _, err := NewSolver([]int{1}, []uint32{0}, 511); err == nil {
		t.Fatal("want error: slice size not a multiple of 4")
	}
}
