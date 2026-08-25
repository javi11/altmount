package par2repair

import (
	"bytes"
	"math/rand"
	"strconv"
	"testing"

	"github.com/javi11/gopar-turbo/gf2p16"
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
	// Zero missing slices is valid: a verify sweep starts empty and grows
	// its unknowns via AddMissing as corrupt slices surface.
	if _, err := NewSolver(nil, nil, 512); err != nil {
		t.Fatalf("zero missing slices must construct a valid solver, got %v", err)
	}
	if _, err := NewSolver([]int{1}, []uint32{0}, 511); err == nil {
		t.Fatal("want error: slice size not a multiple of 4")
	}
}

// TestSolverMarginRowsAndAddMissing loads more recovery rows than there are
// missing slices (margin) and reclassifies one more slice as missing mid-fold,
// as the sweep does when an article dies between planning and sweeping.
func TestSolverMarginRowsAndAddMissing(t *testing.T) {
	const sliceSize, n = 512, 10
	rng := rand.New(rand.NewSource(11))
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = make([]byte, sliceSize)
		rng.Read(slices[j])
	}
	exps := []uint32{0, 1, 2, 3} // 4 rows for what starts as 1 missing slice
	rec := buildRecovery(slices, exps, sliceSize)

	s, err := NewSolver([]int{2}, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rec {
		if err := s.AddRecovery(i, r); err != nil {
			t.Fatal(err)
		}
	}
	for j, sl := range slices {
		if j == 2 {
			continue
		}
		if j == 5 {
			// Slice 5 turns out missing mid-sweep: absorb it instead of
			// re-sweeping, using one of the margin rows.
			if err := s.AddMissing(5); err != nil {
				t.Fatal(err)
			}
			continue
		}
		s.FoldPresent(j, sl)
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("recovered %d slices, want 2", len(got))
	}
	if !bytes.Equal(got[0], slices[2]) || !bytes.Equal(got[1], slices[5]) {
		t.Fatal("margin solve did not recover both slices")
	}
}

// TestSolverAddMissingWithoutSpareRow verifies AddMissing refuses to grow the
// missing set past the loaded recovery rows.
func TestSolverAddMissingWithoutSpareRow(t *testing.T) {
	s, err := NewSolver([]int{0}, []uint32{0}, 512)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddMissing(1); err == nil {
		t.Fatal("want error: no spare accumulator for a second missing slice")
	}
}

// TestSolverSolveSkipsDependentRows gives Solve linearly dependent rows ahead
// of an independent one: it must select an invertible row subset instead of
// failing on the first k rows (the PAR2 Vandermonde singularity case).
func TestSolverSolveSkipsDependentRows(t *testing.T) {
	const sliceSize, n = 256, 8
	rng := rand.New(rand.NewSource(13))
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = make([]byte, sliceSize)
		rng.Read(slices[j])
	}
	// Rows 0 and 1 share an exponent, so any 2x2 system over them is singular;
	// row 2 provides the independent equation.
	exps := []uint32{1, 1, 5}
	rec := buildRecovery(slices, exps, sliceSize)

	missing := []int{0, 4}
	s, err := NewSolver(missing, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rec {
		if err := s.AddRecovery(i, r); err != nil {
			t.Fatal(err)
		}
	}
	for j, sl := range slices {
		if j != 0 && j != 4 {
			s.FoldPresent(j, sl)
		}
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0], slices[0]) || !bytes.Equal(got[1], slices[4]) {
		t.Fatal("row-selecting solve did not recover the slices")
	}
}

// TestSolverSeedRecoveryOwning donates the recovery payload buffer to the
// solver: seeding consumes it into the accumulator's prepared layout and the
// caller must not rely on it afterwards.
func TestSolverSeedRecoveryOwning(t *testing.T) {
	const sliceSize, n = 512, 6
	rng := rand.New(rand.NewSource(17))
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = make([]byte, sliceSize)
		rng.Read(slices[j])
	}
	exps := []uint32{0}
	rec := buildRecovery(slices, exps, sliceSize)

	s, err := NewSolver([]int{3}, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	donated := bytes.Clone(rec[0])
	if err := s.SeedRecoveryOwning(0, donated); err != nil {
		t.Fatal(err)
	}
	for j, sl := range slices {
		if j != 3 {
			s.FoldPresent(j, sl)
		}
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0], slices[3]) {
		t.Fatal("owning-seeded solve did not recover the slice")
	}

	// Wrong-size donations are refused: the arithmetic reads whole slices.
	s2, err := NewSolver([]int{0}, []uint32{0}, 512)
	if err != nil {
		t.Fatal(err)
	}
	if err := s2.SeedRecoveryOwning(0, make([]byte, 100)); err == nil {
		t.Fatal("want error: donated buffer must be exactly one slice")
	}
}

// TestSolverLargeSliceRoundTrip exercises the parallel fold path (slices above
// the parallel threshold, with a size that does not divide evenly across
// workers) and must agree with the spec arithmetic byte for byte.
func TestSolverLargeSliceRoundTrip(t *testing.T) {
	const sliceSize = 192<<10 + 4 // above minParallelFold, awkward chunking
	const n = 5
	rng := rand.New(rand.NewSource(19))
	slices := make([][]byte, n)
	for j := range slices {
		slices[j] = make([]byte, sliceSize)
		rng.Read(slices[j])
	}
	missing := []int{1, 4}
	exps := []uint32{0, 1}
	rec := buildRecovery(slices, exps, sliceSize)

	s, err := NewSolver(missing, exps, sliceSize)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range rec {
		if err := s.AddRecovery(i, r); err != nil {
			t.Fatal(err)
		}
	}
	for j, sl := range slices {
		if j != 1 && j != 4 {
			s.FoldPresent(j, sl)
		}
	}
	got, err := s.Solve()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[0], slices[1]) || !bytes.Equal(got[1], slices[4]) {
		t.Fatal("large-slice round trip failed")
	}
}

func BenchmarkFoldPresent(b *testing.B) {
	const sliceSize = 4 << 20
	slice := make([]byte, sliceSize)
	rand.New(rand.NewSource(23)).Read(slice)
	exps := []uint32{0, 1, 2, 3, 4, 5, 6, 7}
	s, err := NewSolver([]int{0, 1, 2, 3, 4, 5, 6, 7}, exps, sliceSize)
	if err != nil {
		b.Fatal(err)
	}
	for i := range exps {
		if err := s.AddRecovery(i, slice); err != nil {
			b.Fatal(err)
		}
	}
	b.SetBytes(sliceSize)
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		s.FoldPresent(8+i, slice)
	}
}

// TestVerifyHeldOutRowDiscriminates proves the held-out-row diagnostic is
// actually diagnostic: it must accept a correct solve, report "unavailable"
// when no margin row was spared, and reject a solve poisoned by folding a
// present slice at the wrong global index — the very confusion (slice
// numbering / layout drift) it exists to detect.
func TestVerifyHeldOutRowDiscriminates(t *testing.T) {
	const sliceSize, n = 512, 20
	mk := func() [][]byte {
		rng := rand.New(rand.NewSource(7))
		sl := make([][]byte, n)
		for j := range sl {
			sl[j] = make([]byte, sliceSize)
			rng.Read(sl[j])
		}
		return sl
	}
	missing := []int{3, 7, 19}

	t.Run("correct solve is consistent", func(t *testing.T) {
		slices := mk()
		exps := []uint32{0, 1, 2, 5} // one margin row
		s, err := NewSolver(missing, exps, sliceSize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for i, r := range buildRecovery(slices, exps, sliceSize) {
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
		row, checked, ok := s.VerifyHeldOutRow(got)
		if !checked {
			t.Fatal("expected the margin row to be available for checking")
		}
		if !ok {
			t.Fatalf("held-out row %d rejected a correct solve", row)
		}
	})

	t.Run("no margin row is unavailable not failed", func(t *testing.T) {
		slices := mk()
		exps := []uint32{0, 1, 2} // exactly k rows: none held out
		s, err := NewSolver(missing, exps, sliceSize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for i, r := range buildRecovery(slices, exps, sliceSize) {
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
		if _, checked, _ := s.VerifyHeldOutRow(got); checked {
			t.Fatal("expected checked=false when every row went into the solve")
		}
	})

	t.Run("wrong slice numbering is rejected", func(t *testing.T) {
		slices := mk()
		exps := []uint32{0, 1, 2, 5}
		s, err := NewSolver(missing, exps, sliceSize)
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		for i, r := range buildRecovery(slices, exps, sliceSize) {
			s.AddRecovery(i, r)
		}
		// Fold two present slices under each other's global index — exactly
		// what a disagreeing global slice numbering does. Every row still
		// folds self-consistently, so only a held-out row can catch it.
		for j, sl := range slices {
			if j == 3 || j == 7 || j == 19 {
				continue
			}
			switch j {
			case 4:
				s.FoldPresent(5, sl)
			case 5:
				s.FoldPresent(4, sl)
			default:
				s.FoldPresent(j, sl)
			}
		}
		got, err := s.Solve()
		if err != nil {
			t.Fatal(err)
		}
		row, checked, ok := s.VerifyHeldOutRow(got)
		if !checked {
			t.Fatal("expected the margin row to be available for checking")
		}
		if ok {
			t.Fatalf("held-out row %d accepted a solve built on swapped slice indices", row)
		}
	})
}

// Real releases pick slice sizes with awkward alignment: a 4.7 GB set seen in
// production used 2380956 = 4 x 595239 with 595239 odd, so it is a multiple
// of 4 (all NewSolver requires) but of no higher power of two. Every other
// solver test uses a small or well-aligned size, which leaves the SIMD
// backend's prepared layout and the parallel fold's stride-aligned chunk
// split unexercised at the sizes that actually ship.
func TestSolverAwkwardSliceSizes(t *testing.T) {
	for _, sliceSize := range []int{2380956, 1 << 20, 196612, 131072 + 4} {
		t.Run(strconv.Itoa(sliceSize), func(t *testing.T) {
			const n = 12
			rng := rand.New(rand.NewSource(3))
			sl := make([][]byte, n)
			for j := range sl {
				sl[j] = make([]byte, sliceSize)
				rng.Read(sl[j])
			}
			missing := []int{1, 4, 9}
			isMissing := func(j int) bool { return j == 1 || j == 4 || j == 9 }
			exps := []uint32{0, 1, 2, 7, 11}

			s, err := NewSolver(missing, exps, sliceSize)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			for i, r := range buildRecovery(sl, exps, sliceSize) {
				s.AddRecovery(i, r)
			}
			for j, x := range sl {
				if isMissing(j) {
					continue
				}
				s.FoldPresent(j, x)
			}
			got, err := s.Solve()
			if err != nil {
				t.Fatal(err)
			}
			for i, m := range missing {
				if !bytes.Equal(got[i], sl[m]) {
					first := 0
					for first < sliceSize && got[i][first] == sl[m][first] {
						first++
					}
					t.Fatalf("slice %d wrong; first differing byte %d of %d", m, first, sliceSize)
				}
			}
			if row, checked, ok := s.VerifyHeldOutRow(got); checked && !ok {
				t.Fatalf("held-out row %d rejected a correct solve", row)
			}
		})
	}
}
