package par2

import (
	"bytes"
	"errors"
	"sort"
)

var (
	ErrInsufficientRecovery = errors.New("par2: not enough recovery slices to reconstruct missing blocks")
	ErrSingularMatrix       = errors.New("par2: recovery matrix is singular")
	ErrBlockCountMismatch   = errors.New("par2: present-block slice does not match recovery-set layout")
)

func gcdInt(a, b int) int {
	for b != 0 {
		a, b = b, a%b
	}
	return a
}

// inputBases returns the PAR2 base constant for each of the first n input
// blocks. Per the spec, the base for input block i is 2^logbase where logbase
// walks the integers whose value is coprime to 65535 (i.e. not divisible by 3,
// 5, 17, or 257). This guarantees every square submatrix of the RS matrix is
// invertible.
func inputBases(n int) ([]uint16, error) {
	bases := make([]uint16, n)
	logbase := 0
	for i := 0; i < n; i++ {
		for logbase < gfMax && gcdInt(gfMax, logbase) != 1 {
			logbase++
		}
		if logbase >= gfMax {
			return nil, errors.New("par2: too many input blocks for the Reed-Solomon matrix")
		}
		bases[i] = gfExp[logbase] // ALog(logbase) == 2^logbase
		logbase++
	}
	return bases, nil
}

// FileLayout maps a recovery-set file to its global input-block range.
type FileLayout struct {
	ID    [16]byte
	Start int // global index of this file's first slice
	Count int // number of slices for this file
}

// Layout returns the global ordering of input blocks across the recovery set
// (files sorted by File ID, slices in order) and the total block count. The
// global index is what the base-constant assignment is keyed on.
func (rs *RecoverySet) Layout() ([]FileLayout, int) {
	ids := append([][16]byte(nil), rs.RecoveryFileIDs...)
	sort.Slice(ids, func(i, j int) bool { return bytes.Compare(ids[i][:], ids[j][:]) < 0 })

	ss := int(rs.SliceSize)
	var layout []FileLayout
	total := 0
	for _, id := range ids {
		fd := rs.Files[id]
		if fd == nil {
			continue
		}
		count := int((fd.Length + uint64(ss) - 1) / uint64(ss))
		layout = append(layout, FileLayout{ID: id, Start: total, Count: count})
		total += count
	}
	return layout, total
}

// Reconstruct recovers any missing input blocks. present is indexed by global
// input-block index (see Layout): a non-nil entry is the known slice data
// (exactly SliceSize bytes, zero-padded), a nil entry is a block to recover. It
// returns a fully populated slice with the recovered blocks filled in.
func (rs *RecoverySet) Reconstruct(present [][]byte) ([][]byte, error) {
	ss := int(rs.SliceSize)
	n := len(present)

	bases, err := inputBases(n)
	if err != nil {
		return nil, err
	}

	var missing []int
	for i, b := range present {
		if b == nil {
			missing = append(missing, i)
		} else if len(b) != ss {
			return nil, ErrBlockCountMismatch
		}
	}
	if len(missing) == 0 {
		return present, nil
	}
	k := len(missing)
	if len(rs.Recovery) < k {
		return nil, ErrInsufficientRecovery
	}
	recs := rs.Recovery[:k] // any k recovery slices suffice (matrix is non-singular)

	// Coefficient matrix A[r][c] = base(missing[c]) ^ exponent(recs[r]).
	A := make([][]uint16, k)
	for r := 0; r < k; r++ {
		A[r] = make([]uint16, k)
		for c := 0; c < k; c++ {
			A[r][c] = gfPow(bases[missing[c]], int(recs[r].Exponent))
		}
	}

	// Right-hand side: for each chosen recovery slice, subtract (XOR) the
	// contribution of every block we already have, leaving only the missing
	// blocks' weighted sum.
	rhs := make([][]byte, k)
	for r := 0; r < k; r++ {
		e := int(recs[r].Exponent)
		row := make([]byte, ss)
		copy(row, recs[r].Data)
		for i, b := range present {
			if b == nil {
				continue
			}
			mulSliceInto(row, b, gfPow(bases[i], e))
		}
		rhs[r] = row
	}

	if err := gaussSolve(A, rhs); err != nil {
		return nil, err
	}

	out := make([][]byte, n)
	copy(out, present)
	for c, idx := range missing {
		out[idx] = rhs[c]
	}
	return out, nil
}

// gaussSolve solves A·x = rhs over GF(2^16) by Gauss-Jordan elimination,
// applying the same row operations to the rhs slice-vectors. On return, rhs[i]
// holds x[i] (the recovered slice for missing block i).
func gaussSolve(A [][]uint16, rhs [][]byte) error {
	k := len(A)
	for p := 0; p < k; p++ {
		pivot := -1
		for r := p; r < k; r++ {
			if A[r][p] != 0 {
				pivot = r
				break
			}
		}
		if pivot < 0 {
			return ErrSingularMatrix
		}
		A[p], A[pivot] = A[pivot], A[p]
		rhs[p], rhs[pivot] = rhs[pivot], rhs[p]

		inv := gfInv(A[p][p])
		for j := 0; j < k; j++ {
			A[p][j] = gfMul(A[p][j], inv)
		}
		scaleSlice(rhs[p], inv)

		for r := 0; r < k; r++ {
			if r == p {
				continue
			}
			f := A[r][p]
			if f == 0 {
				continue
			}
			for j := 0; j < k; j++ {
				A[r][j] = gfAdd(A[r][j], gfMul(f, A[p][j]))
			}
			mulSliceInto(rhs[r], rhs[p], f) // rhs[r] ^= f * rhs[p]
		}
	}
	return nil
}

// scaleSlice computes b = c * b over little-endian GF(2^16) words, in place.
func scaleSlice(b []byte, c uint16) {
	if c == 1 {
		return
	}
	if c == 0 {
		for i := range b {
			b[i] = 0
		}
		return
	}
	lc := int(gfLog[c])
	for i := 0; i+1 < len(b); i += 2 {
		v := uint16(b[i]) | uint16(b[i+1])<<8
		if v != 0 {
			p := gfExp[(int(gfLog[v])+lc)%gfMax]
			b[i] = byte(p)
			b[i+1] = byte(p >> 8)
		}
	}
}
