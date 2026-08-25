package par2_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	"github.com/javi11/altmount/internal/par2repair"
)

// The realtool fixture was produced by par2cmdline (par2 create -s1024 -c4
// -n1) over two deterministic input files. It pins our parser and solver to a
// real, external PAR2 implementation rather than our own generator.
const realToolDir = "testdata/realtool"

// loadRealToolSet reads the fixture .par2 files (index first, volumes after,
// sorted by filename) and returns the parsed index plus the raw bytes of each
// stream in the exact order they were handed to ParseIndex, so that
// RecoverySliceRef.FileIndex can be used to index into them.
func loadRealToolSet(t *testing.T) (*par2.Index, [][]byte) {
	t.Helper()

	entries, err := filepath.Glob(filepath.Join(realToolDir, "*.par2"))
	if err != nil {
		t.Fatalf("glob fixture: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least an index and one volume file, got %v", entries)
	}
	sort.Strings(entries) // "realtool.par2" sorts before "realtool.vol0+4.par2"

	raw := make([][]byte, len(entries))
	streams := make([]io.Reader, len(entries))
	for i, name := range entries {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		raw[i] = b
		streams[i] = bytes.NewReader(b)
	}

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	return idx, raw
}

// loadInput reads one of the fixture's original input files.
func loadInput(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(realToolDir, name))
	if err != nil {
		t.Fatalf("read input %s: %v", name, err)
	}
	return b
}

func TestParseRealToolFixture(t *testing.T) {
	idx, _ := loadRealToolSet(t)

	if idx.SliceSize != 1024 {
		t.Errorf("SliceSize = %d, want 1024", idx.SliceSize)
	}
	if len(idx.RecoveryIDs) != 2 {
		t.Fatalf("recovery-set members = %d, want 2", len(idx.RecoveryIDs))
	}

	wantFiles := map[string]uint64{
		"fileA.bin": 8000,
		"fileB.bin": 5600,
	}
	for _, id := range idx.RecoveryIDs {
		fd, ok := idx.Files[id]
		if !ok {
			t.Fatalf("recovery-set member %x has no FileDesc", id)
		}
		wantLen, ok := wantFiles[fd.Name]
		if !ok {
			t.Fatalf("unexpected recovery-set file %q", fd.Name)
		}
		if fd.Length != wantLen {
			t.Errorf("file %q length = %d, want %d", fd.Name, fd.Length, wantLen)
		}
		delete(wantFiles, fd.Name)

		wantSlices := int((fd.Length + idx.SliceSize - 1) / idx.SliceSize)
		if got := len(idx.SliceChecks[id]); got != wantSlices {
			t.Errorf("file %q IFSC slice count = %d, want %d", fd.Name, got, wantSlices)
		}
	}
	if len(wantFiles) != 0 {
		t.Errorf("recovery set missing files: %v", wantFiles)
	}

	if len(idx.Recovery) != 4 {
		t.Fatalf("recovery slices = %d, want 4", len(idx.Recovery))
	}
	seen := make(map[uint32]bool)
	for _, ref := range idx.Recovery {
		if seen[ref.Exponent] {
			t.Errorf("duplicate recovery exponent %d", ref.Exponent)
		}
		seen[ref.Exponent] = true
	}
}

// TestSolveAgainstRealToolRecovery proves our GF(2^16) math matches
// par2cmdline: it reconstructs a "missing" input slice using only a recovery
// payload emitted by the real tool plus the remaining present slices, and
// byte-compares the result against the true slice content.
func TestSolveAgainstRealToolRecovery(t *testing.T) {
	idx, raw := loadRealToolSet(t)
	sliceSize := int(idx.SliceSize)

	// Build the global slice array in recovery-set order (RecoveryIDs is
	// FileID-ascending, matching the PAR2 spec's global slice numbering).
	var slices [][]byte
	for _, id := range idx.RecoveryIDs {
		fd := idx.Files[id]
		content := loadInput(t, fd.Name)
		if uint64(len(content)) != fd.Length {
			t.Fatalf("input %q is %d bytes, FileDesc says %d", fd.Name, len(content), fd.Length)
		}
		for off := 0; off < len(content); off += sliceSize {
			s := make([]byte, sliceSize) // zero-padded
			copy(s, content[off:])
			slices = append(slices, s)
		}
	}

	const missingIdx = 5 // arbitrary slice to reconstruct

	// Extract one recovery payload straight from the real tool's file bytes.
	// It must have a non-zero exponent: with exponent 0 every coefficient is
	// g^0 = 1, so the equation degenerates to an XOR of all slices and the
	// solve stops depending on each slice's global index -- blinding the test
	// to slice-numbering errors.
	ref := nonZeroExponentRef(t, idx)
	stream := raw[ref.FileIndex]
	if int64(len(stream)) < ref.BodyOffset+int64(sliceSize) {
		t.Fatalf("recovery payload out of bounds: stream %d has %d bytes, need offset %d + %d",
			ref.FileIndex, len(stream), ref.BodyOffset, sliceSize)
	}
	payload := stream[ref.BodyOffset : ref.BodyOffset+int64(sliceSize)]

	solver, err := par2repair.NewSolver([]int{missingIdx}, []uint32{ref.Exponent}, sliceSize)
	if err != nil {
		t.Fatalf("NewSolver: %v", err)
	}
	solver.AddRecovery(0, payload)
	for i, s := range slices {
		if i == missingIdx {
			continue
		}
		solver.FoldPresent(i, s)
	}

	out, err := solver.Solve()
	if err != nil {
		t.Fatalf("Solve: %v", err)
	}
	if !bytes.Equal(out[0], slices[missingIdx]) {
		t.Fatalf("recovered slice %d does not match real content:\n got %x...\nwant %x...",
			missingIdx, out[0][:32], slices[missingIdx][:32])
	}
}

// realToolLargeDir holds a second par2cmdline fixture (par2 create -s131072
// -c2 -n1 over two deterministic input files) whose slice size clears
// minParallelFold (128 KiB): unlike realToolDir's 1024-byte slices, folding
// here exercises the parallel stride-split path and, on a cgo build, the
// ParPar SIMD prepared-layout kernels — the code every real repair actually
// runs, and the one path TestSolveAgainstRealToolRecovery does not reach.
const realToolLargeDir = "testdata/realtool_large"

func loadRealToolLargeSet(t *testing.T) (*par2.Index, [][]byte) {
	t.Helper()

	entries, err := filepath.Glob(filepath.Join(realToolLargeDir, "*.par2"))
	if err != nil {
		t.Fatalf("glob fixture: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least an index and one volume file, got %v", entries)
	}
	sort.Strings(entries)

	raw := make([][]byte, len(entries))
	streams := make([]io.Reader, len(entries))
	for i, name := range entries {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		raw[i] = b
		streams[i] = bytes.NewReader(b)
	}

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	return idx, raw
}

// TestSolveAgainstRealToolRecoveryParallelFold is
// TestSolveAgainstRealToolRecovery at a slice size that clears
// minParallelFold, seeded the way RunJob actually seeds accumulators
// (SeedRecoveryOwning, not AddRecovery), so a bug confined to the parallel
// fold or the SIMD backend's prepared layout shows up here even when the
// small-slice fixture passes.
func TestSolveAgainstRealToolRecoveryParallelFold(t *testing.T) {
	idx, raw := loadRealToolLargeSet(t)
	sliceSize := int(idx.SliceSize)
	if sliceSize < 128<<10 {
		t.Fatalf("fixture slice size %d does not clear the parallel-fold threshold", sliceSize)
	}

	var slices [][]byte
	for _, id := range idx.RecoveryIDs {
		fd := idx.Files[id]
		content, err := os.ReadFile(filepath.Join(realToolLargeDir, fd.Name))
		if err != nil {
			t.Fatalf("read input %s: %v", fd.Name, err)
		}
		if uint64(len(content)) != fd.Length {
			t.Fatalf("input %q is %d bytes, FileDesc says %d", fd.Name, len(content), fd.Length)
		}
		for off := 0; off < len(content); off += sliceSize {
			s := make([]byte, sliceSize)
			copy(s, content[off:])
			slices = append(slices, s)
		}
	}
	if len(slices) < 3 {
		t.Fatalf("fixture has only %d slices, want enough to exercise more than one file", len(slices))
	}

	for _, missingIdx := range []int{0, len(slices) - 1} {
		t.Run(fmt.Sprintf("missing_%d", missingIdx), func(t *testing.T) {
			ref := nonZeroExponentRef(t, idx)
			stream := raw[ref.FileIndex]
			if int64(len(stream)) < ref.BodyOffset+int64(sliceSize) {
				t.Fatalf("recovery payload out of bounds: stream %d has %d bytes, need offset %d + %d",
					ref.FileIndex, len(stream), ref.BodyOffset, sliceSize)
			}
			payload := make([]byte, sliceSize)
			copy(payload, stream[ref.BodyOffset:ref.BodyOffset+int64(sliceSize)])

			solver, err := par2repair.NewSolver([]int{missingIdx}, []uint32{ref.Exponent}, sliceSize)
			if err != nil {
				t.Fatalf("NewSolver: %v", err)
			}
			defer solver.Close()
			if err := solver.SeedRecoveryOwning(0, payload); err != nil {
				t.Fatalf("SeedRecoveryOwning: %v", err)
			}
			for i, s := range slices {
				if i == missingIdx {
					continue
				}
				solver.FoldPresent(i, s)
			}

			out, err := solver.Solve()
			if err != nil {
				t.Fatalf("Solve: %v", err)
			}
			if !bytes.Equal(out[0], slices[missingIdx]) {
				t.Fatalf("recovered slice %d does not match real content:\n got %x...\nwant %x...",
					missingIdx, out[0][:32], slices[missingIdx][:32])
			}
		})
	}
}

// orderCheckDir holds a par2cmdline fixture with TEN input files (par2 create
// -s2048 -c4 -n1). File count is the whole point: with only two members, a
// lexicographic FileID sort and PAR2's own ordering agree about half the time,
// so the two-file fixtures above cannot detect a wrong comparator. At ten
// members they diverge, which is what a real 47-volume release looks like.
const orderCheckDir = "testdata/ordercheck"

func loadOrderCheckSet(t *testing.T) (*par2.Index, [][]byte) {
	t.Helper()
	entries, err := filepath.Glob(filepath.Join(orderCheckDir, "*.par2"))
	if err != nil {
		t.Fatalf("glob fixture: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected an index and a volume file, got %v", entries)
	}
	sort.Strings(entries)
	raw := make([][]byte, len(entries))
	streams := make([]io.Reader, len(entries))
	for i, name := range entries {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		raw[i] = b
		streams[i] = bytes.NewReader(b)
	}
	idx, err := par2.ParseIndex(streams)
	if err != nil {
		t.Fatalf("ParseIndex: %v", err)
	}
	return idx, raw
}

// The recovery-set order must be the Main packet's stored order. PAR2 orders
// FileIDs with byte 15 most significant (par2cmdline's MD5Hash::operator<),
// so a lexicographic bytes.Compare sort produces a DIFFERENT permutation --
// which silently rebases every file's global slice index.
func TestRecoveryIDsPreserveMainPacketOrder(t *testing.T) {
	idx, _ := loadOrderCheckSet(t)
	if len(idx.RecoveryIDs) != 10 {
		t.Fatalf("recovery-set members = %d, want 10", len(idx.RecoveryIDs))
	}

	// Read the stored order straight out of the Main packet bytes. Comparing
	// against a re-sort of idx.RecoveryIDs would be circular: if ParseIndex
	// re-sorted them, the re-sort is a no-op and the check passes vacuously.
	stored := storedMainPacketIDs(t, filepath.Join(orderCheckDir, "oc.par2"))
	if len(stored) != len(idx.RecoveryIDs) {
		t.Fatalf("stored %d IDs, index has %d", len(stored), len(idx.RecoveryIDs))
	}

	lexicographic := make([][16]byte, len(stored))
	copy(lexicographic, stored)
	sort.Slice(lexicographic, func(i, j int) bool {
		return bytes.Compare(lexicographic[i][:], lexicographic[j][:]) < 0
	})
	if slicesEqual(lexicographic, stored) {
		t.Fatal("fixture is useless: its lexicographic and PAR2 orderings coincide")
	}

	if !slicesEqual(stored, idx.RecoveryIDs) {
		t.Errorf("RecoveryIDs were reordered; global slice numbering is wrong.\nstored[0..3] = %x %x %x\n  index[0..3] = %x %x %x",
			stored[0][:4], stored[1][:4], stored[2][:4],
			idx.RecoveryIDs[0][:4], idx.RecoveryIDs[1][:4], idx.RecoveryIDs[2][:4])
	}
	if !idx.MainIDsWereSorted {
		t.Error("par2cmdline writes Main packet IDs in PAR2 order; MainIDsWereSorted should be true")
	}
}

// storedMainPacketIDs parses a .par2 file's Main packet and returns the
// recovery-set FileIDs exactly as stored, independent of ParseIndex.
func storedMainPacketIDs(t *testing.T, path string) [][16]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	magic := []byte("PAR2\x00PKT")
	for i := 0; i+64 <= len(data); {
		j := bytes.Index(data[i:], magic)
		if j < 0 {
			break
		}
		j += i
		if j+64 > len(data) {
			break
		}
		length := binary.LittleEndian.Uint64(data[j+8 : j+16])
		if length < 64 || j+int(length) > len(data) {
			i = j + 1
			continue
		}
		if string(data[j+48:j+64]) == "PAR 2.0\x00Main\x00\x00\x00\x00" {
			body := data[j+64 : j+int(length)]
			n := int(binary.LittleEndian.Uint32(body[8:12]))
			out := make([][16]byte, n)
			for k := range out {
				copy(out[k][:], body[12+16*k:12+16*(k+1)])
			}
			return out
		}
		i = j + 1
	}
	t.Fatalf("no Main packet found in %s", path)
	return nil
}

func slicesEqual(a, b [][16]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// End-to-end proof on ten real par2cmdline files: reconstruct a slice from a
// real recovery payload using the global numbering ParseIndex hands us. With
// the FileID comparator wrong, every file after the first permuted one gets
// the wrong slice base, so this solve produces garbage.
func TestSolveAcrossManyFilesUsesCorrectSliceNumbering(t *testing.T) {
	idx, raw := loadOrderCheckSet(t)
	sliceSize := int(idx.SliceSize)

	var slices [][]byte
	var owner []string
	for _, id := range idx.RecoveryIDs {
		fd := idx.Files[id]
		content, err := os.ReadFile(filepath.Join(orderCheckDir, fd.Name))
		if err != nil {
			t.Fatalf("read input %s: %v", fd.Name, err)
		}
		if uint64(len(content)) != fd.Length {
			t.Fatalf("input %q is %d bytes, FileDesc says %d", fd.Name, len(content), fd.Length)
		}
		for off := 0; off < len(content); off += sliceSize {
			s := make([]byte, sliceSize)
			copy(s, content[off:])
			slices = append(slices, s)
			owner = append(owner, fd.Name)
		}
	}
	t.Logf("%d members, %d global slices, slice size %d", len(idx.RecoveryIDs), len(slices), sliceSize)

	// Probe slices spread across the permuted range, including ones deep into
	// later files where a mis-ordering has fully diverged.
	for _, missingIdx := range []int{0, len(slices) / 3, len(slices) / 2, len(slices) - 1} {
		t.Run(fmt.Sprintf("slice_%d_in_%s", missingIdx, owner[missingIdx]), func(t *testing.T) {
			// A non-zero exponent is essential. With exponent 0 every
			// Vandermonde coefficient is g^0 = 1, the equation collapses to a
			// plain XOR of all slices, and the result no longer depends on
			// which global index each slice was folded under -- so an
			// exponent-0 row cannot detect a slice-numbering error at all.
			ref := nonZeroExponentRef(t, idx)
			stream := raw[ref.FileIndex]
			if int64(len(stream)) < ref.BodyOffset+int64(sliceSize) {
				t.Fatalf("recovery payload out of bounds")
			}
			payload := make([]byte, sliceSize)
			copy(payload, stream[ref.BodyOffset:ref.BodyOffset+int64(sliceSize)])

			solver, err := par2repair.NewSolver([]int{missingIdx}, []uint32{ref.Exponent}, sliceSize)
			if err != nil {
				t.Fatal(err)
			}
			defer solver.Close()
			solver.AddRecovery(0, payload)
			for i, s := range slices {
				if i == missingIdx {
					continue
				}
				solver.FoldPresent(i, s)
			}
			out, err := solver.Solve()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(out[0], slices[missingIdx]) {
				t.Fatalf("recovered slice %d (%s) does not match real content:\n got %x...\nwant %x...",
					missingIdx, owner[missingIdx], out[0][:16], slices[missingIdx][:16])
			}
		})
	}
}

// nonZeroExponentRef returns a recovery slice whose exponent is not zero, so
// its coefficients actually depend on each slice's global index.
func nonZeroExponentRef(t *testing.T, idx *par2.Index) par2.RecoverySliceRef {
	t.Helper()
	for _, r := range idx.Recovery {
		if r.Exponent != 0 {
			return r
		}
	}
	t.Fatal("fixture has no recovery slice with a non-zero exponent")
	return par2.RecoverySliceRef{}
}
