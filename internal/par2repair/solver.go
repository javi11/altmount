// Package par2repair reconstructs missing usenet articles from a release's
// PAR2 recovery data, streaming the recovery set from NNTP and persisting
// repaired article payloads to a local patch store.
package par2repair

import (
	"errors"
	"fmt"
	"runtime"
	"slices"
	"sync"

	"github.com/akalin/gopar/gf2p16"
)

// ErrSingularMatrix is returned by Solve when no invertible subset of the
// loaded recovery rows exists — a known flaw of the PAR2 Vandermonde
// construction. Retrying with a different recovery-slice subset may succeed.
var ErrSingularMatrix = errors.New("par2repair: recovery matrix is singular")

// Solver reconstructs missing slices from a single streaming pass over the
// present slices plus the loaded recovery slices.
//
// Math: recovery slice r with exponent e_r satisfies
//
//	R_r = Σ_{j present} g_j^{e_r}·D_j ⊕ Σ_{m missing} g_m^{e_r}·D_m
//
// Each accumulator is seeded with R_r (AddRecovery or SeedRecoveryOwning) and
// every present slice is folded in (FoldPresent); what remains is the
// missing-only combination, solved as a k×k system in Solve.
//
// More recovery rows than missing slices may be loaded (margin rows): every
// row is folded, and a slice discovered missing mid-fold joins the unknowns
// via AddMissing without a second pass. Solve then uses as many rows as there
// are unknowns, choosing a linearly independent subset.
//
// Not safe for concurrent use: all calls must come from one goroutine.
type Solver struct {
	missing   []int
	exps      []uint32
	acc       [][]byte // one per exps entry; nil until seeded
	sliceSize int
	alloc     bufAlloc
	workers   int
}

// bufAlloc returns a zeroed buffer of n bytes. The heap allocator is the
// default; disk-backed jobs pass an arena allocator instead.
type bufAlloc func(n int) ([]byte, error)

func heapAlloc(n int) ([]byte, error) { return make([]byte, n), nil }

// minParallelFold is the slice size below which folding stays on one
// goroutine. Splitting a small slice costs more in scheduling than the fold
// itself takes.
const minParallelFold = 128 << 10

// NewSolver prepares a solver for the given missing global slice indices and
// the exponents of the recovery slices that will seed the accumulators — at
// least one per missing slice; extra rows are margin for slices discovered
// missing after folding began.
func NewSolver(missingIdx []int, recoveryExp []uint32, sliceSize int) (*Solver, error) {
	return NewSolverAlloc(missingIdx, recoveryExp, sliceSize, heapAlloc)
}

// NewSolverAlloc is NewSolver with the accumulator (and recovered-slice)
// buffers coming from alloc, which must return zeroed memory.
func NewSolverAlloc(missingIdx []int, recoveryExp []uint32, sliceSize int, alloc bufAlloc) (*Solver, error) {
	if len(missingIdx) == 0 || len(recoveryExp) < len(missingIdx) {
		return nil, fmt.Errorf("par2repair: need at least one recovery slice per missing slice (missing=%d recovery=%d)",
			len(missingIdx), len(recoveryExp))
	}
	if sliceSize <= 0 || sliceSize%4 != 0 {
		return nil, fmt.Errorf("par2repair: invalid slice size %d", sliceSize)
	}
	return &Solver{
		missing:   slices.Clone(missingIdx),
		exps:      slices.Clone(recoveryExp),
		acc:       make([][]byte, len(recoveryExp)),
		sliceSize: sliceSize,
		alloc:     alloc,
		workers:   runtime.GOMAXPROCS(0),
	}, nil
}

// AddRecovery seeds accumulator i with a copy of the i-th recovery slice's
// payload; the caller's buffer stays usable. A caller that read the payload
// for this repair alone should donate it via SeedRecoveryOwning instead and
// skip the copy.
func (s *Solver) AddRecovery(i int, payload []byte) error {
	if s.acc[i] == nil {
		buf, err := s.alloc(s.sliceSize)
		if err != nil {
			return err
		}
		s.acc[i] = buf
	}
	gf2p16.MulAndAddByteSliceLE(1, payload, s.acc[i][:len(payload)])
	return nil
}

// SeedRecoveryOwning seeds accumulator i by taking the payload buffer as the
// accumulator itself. The fold works in it and destroys it: after this the
// buffer holds the solver's working state, and nothing may read it again as
// the recovery slice it arrived as. The buffer must be exactly one slice —
// the arithmetic reads and writes whole slices.
func (s *Solver) SeedRecoveryOwning(i int, payload []byte) error {
	if len(payload) != s.sliceSize {
		return fmt.Errorf("par2repair: donated recovery buffer is %d bytes, want %d", len(payload), s.sliceSize)
	}
	if s.acc[i] != nil {
		return fmt.Errorf("par2repair: accumulator %d already seeded", i)
	}
	s.acc[i] = payload
	return nil
}

// AddMissing reclassifies one more global slice as missing, consuming a
// margin row. Call it instead of FoldPresent when a slice the plan thought
// present turns out unreadable or corrupt mid-fold; the slice must not have
// been folded. Fails when every loaded recovery row is already spoken for.
func (s *Solver) AddMissing(globalIdx int) error {
	if len(s.missing) >= len(s.exps) {
		return fmt.Errorf("par2repair: no spare recovery row for slice %d (missing=%d rows=%d)",
			globalIdx, len(s.missing)+1, len(s.exps))
	}
	s.missing = append(s.missing, globalIdx)
	return nil
}

// FoldPresent folds one present input slice (zero-padded to slice size) into
// every accumulator. Slices may arrive in any order. All accumulators must be
// seeded first.
func (s *Solver) FoldPresent(globalIdx int, slice []byte) {
	g := VandermondeBase(globalIdx)

	if s.workers > 1 && len(slice) >= minParallelFold {
		// The accumulators are independent, so the fold parallelises cleanly.
		// Splitting by byte range rather than by row keeps every worker on one
		// span of the source across all rows — that span stays in cache for
		// the whole pass — and keeps all cores busy even when there are fewer
		// recovery rows than cores. Chunks stay on 4-byte boundaries, since
		// the field arithmetic works in 16-bit words.
		chunk := (len(slice) + s.workers - 1) / s.workers
		chunk += (4 - chunk%4) % 4

		var wg sync.WaitGroup
		for from := 0; from < len(slice); from += chunk {
			to := min(from+chunk, len(slice))
			wg.Add(1)
			go func(from, to int) {
				defer wg.Done()
				s.foldRange(g, slice, from, to)
			}(from, to)
		}
		wg.Wait()
		return
	}

	s.foldRange(g, slice, 0, len(slice))
}

// foldRange folds one span of a present slice into every accumulator.
func (s *Solver) foldRange(g gf2p16.T, slice []byte, from, to int) {
	src := slice[from:to]
	for r := range s.acc {
		gf2p16.MulAndAddByteSliceLE(g.Pow(s.exps[r]), src, s.acc[r][from:to])
	}
}

// Solve inverts a k×k system over a linearly independent subset of the loaded
// rows and returns the recovered slices, positionally matching the missing
// indices (NewSolver's, plus AddMissing's in call order).
func (s *Solver) Solve() ([][]byte, error) {
	k := len(s.missing)

	// Coefficient of unknown i in row r.
	coeff := func(r, i int) gf2p16.T {
		return VandermondeBase(s.missing[i]).Pow(s.exps[r])
	}
	rows := selectIndependentRows(len(s.exps), k, coeff)
	if rows == nil {
		return nil, ErrSingularMatrix
	}

	a := gf2p16.NewMatrixFromFunction(k, k, func(r, i int) gf2p16.T {
		return coeff(rows[r], i)
	})
	inv, err := a.Inverse()
	if err != nil {
		return nil, ErrSingularMatrix
	}
	out := make([][]byte, k)
	for i := range out {
		buf, err := s.alloc(s.sliceSize)
		if err != nil {
			return nil, err
		}
		out[i] = buf
		gf2p16.MulByteSliceLE(inv.At(i, 0), s.acc[rows[0]], out[i])
		for r := 1; r < k; r++ {
			gf2p16.MulAndAddByteSliceLE(inv.At(i, r), s.acc[rows[r]], out[i])
		}
	}
	return out, nil
}

// selectIndependentRows returns the indices of the first k rows (of n, in
// order, coefficients via coeff) that form a linearly independent set, or nil
// when no such set exists. This is what turns margin rows into singular-matrix
// insurance: a dependent row is skipped instead of failing the solve.
func selectIndependentRows(n, k int, coeff func(r, i int) gf2p16.T) []int {
	var sel []int
	var basis [][]gf2p16.T // reduced selected rows
	var pivot []int        // pivot column of each basis row
	for r := 0; r < n && len(sel) < k; r++ {
		v := make([]gf2p16.T, k)
		for i := range v {
			v[i] = coeff(r, i)
		}
		for b, p := range pivot {
			if v[p] == 0 {
				continue
			}
			f := v[p].Div(basis[b][p])
			for c := range v {
				v[c] = v[c].Minus(f.Times(basis[b][c]))
			}
		}
		pc := -1
		for c, x := range v {
			if x != 0 {
				pc = c
				break
			}
		}
		if pc < 0 {
			continue // dependent on the rows already selected
		}
		sel = append(sel, r)
		basis = append(basis, v)
		pivot = append(pivot, pc)
	}
	if len(sel) < k {
		return nil
	}
	return sel
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
