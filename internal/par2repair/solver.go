// Package par2repair reconstructs missing usenet articles from a release's
// PAR2 recovery data, streaming the recovery set from NNTP and persisting
// repaired article payloads to a local patch store.
package par2repair

import (
	"errors"
	"fmt"
	"slices"
	"sync"

	"github.com/akalin/gopar/gf2p16"
)

// ErrSingularMatrix is returned by Solve when the chosen recovery slices
// yield a non-invertible system — a known flaw of the PAR2 Vandermonde
// construction. Retrying with a different recovery-slice subset may succeed.
var ErrSingularMatrix = errors.New("par2repair: recovery matrix is singular")

// Solver reconstructs k missing slices from a single streaming pass over the
// present slices plus k recovery slices.
//
// Math: recovery slice r with exponent e_r satisfies
//
//	R_r = Σ_{j present} g_j^{e_r}·D_j ⊕ Σ_{m missing} g_m^{e_r}·D_m
//
// Each accumulator is seeded with R_r (AddRecovery) and every present slice
// is folded in (FoldPresent); what remains is the missing-only combination,
// solved as a k×k system in Solve.
//
// Not safe for concurrent use: all calls must come from one goroutine.
type Solver struct {
	missing []int
	exps    []uint32
	acc     [][]byte
	alloc   bufAlloc
}

// bufAlloc returns a zeroed buffer of n bytes. The heap allocator is the
// default; disk-backed jobs pass an arena allocator instead.
type bufAlloc func(n int) ([]byte, error)

func heapAlloc(n int) ([]byte, error) { return make([]byte, n), nil }

// NewSolver prepares heap accumulators for the given missing global slice
// indices and the exponents of the recovery slices that will seed them (one
// per missing slice, matched by position).
func NewSolver(missingIdx []int, recoveryExp []uint32, sliceSize int) (*Solver, error) {
	return NewSolverAlloc(missingIdx, recoveryExp, sliceSize, heapAlloc)
}

// NewSolverAlloc is NewSolver with the accumulator (and recovered-slice)
// buffers coming from alloc, which must return zeroed memory.
func NewSolverAlloc(missingIdx []int, recoveryExp []uint32, sliceSize int, alloc bufAlloc) (*Solver, error) {
	if len(missingIdx) == 0 || len(missingIdx) != len(recoveryExp) {
		return nil, fmt.Errorf("par2repair: need exactly one recovery slice per missing slice (missing=%d recovery=%d)",
			len(missingIdx), len(recoveryExp))
	}
	if sliceSize <= 0 || sliceSize%4 != 0 {
		return nil, fmt.Errorf("par2repair: invalid slice size %d", sliceSize)
	}
	acc := make([][]byte, len(missingIdx))
	for i := range acc {
		var err error
		if acc[i], err = alloc(sliceSize); err != nil {
			return nil, err
		}
	}
	return &Solver{
		missing: slices.Clone(missingIdx),
		exps:    slices.Clone(recoveryExp),
		acc:     acc,
		alloc:   alloc,
	}, nil
}

// AddRecovery seeds accumulator i with the i-th chosen recovery slice's
// payload (XOR, since accumulators start zeroed).
func (s *Solver) AddRecovery(i int, payload []byte) {
	gf2p16.MulAndAddByteSliceLE(1, payload, s.acc[i])
}

// FoldPresent folds one present input slice (zero-padded to slice size) into
// every accumulator. Slices may arrive in any order.
func (s *Solver) FoldPresent(globalIdx int, slice []byte) {
	g := VandermondeBase(globalIdx)
	for r := range s.acc {
		gf2p16.MulAndAddByteSliceLE(g.Pow(s.exps[r]), slice, s.acc[r])
	}
}

// Solve inverts the k×k system and returns the recovered slices, positionally
// matching the missing indices given to NewSolver.
func (s *Solver) Solve() ([][]byte, error) {
	k := len(s.missing)
	a := gf2p16.NewMatrixFromFunction(k, k, func(r, i int) gf2p16.T {
		return VandermondeBase(s.missing[i]).Pow(s.exps[r])
	})
	inv, err := a.Inverse()
	if err != nil {
		return nil, ErrSingularMatrix
	}
	out := make([][]byte, k)
	for i := range out {
		buf, err := s.alloc(len(s.acc[0]))
		if err != nil {
			return nil, err
		}
		out[i] = buf
		gf2p16.MulByteSliceLE(inv.At(i, 0), s.acc[0], out[i])
		for r := 1; r < k; r++ {
			gf2p16.MulAndAddByteSliceLE(inv.At(i, r), s.acc[r], out[i])
		}
	}
	return out, nil
}

// VandermondeBase returns the j-th PAR2 Vandermonde generator: 2^i in
// GF(2^16), skipping exponents divisible by 3, 5, 17 or 257 (whose elements
// have order below 65535). The table grows on demand and is process-cached.
func VandermondeBase(j int) gf2p16.T {
	basesMu.Lock()
	defer basesMu.Unlock()
	for len(bases) <= j {
		i := nextBaseExp
		nextBaseExp++
		if i%3 == 0 || i%5 == 0 || i%17 == 0 || i%257 == 0 {
			continue
		}
		bases = append(bases, gf2p16.T(2).Pow(uint32(i)))
	}
	return bases[j]
}

var (
	basesMu     sync.Mutex
	bases       []gf2p16.T
	nextBaseExp int
)
