# Extracting the PAR2 repair engine into gopar-turbo

Date: 2026-08-25
Status: approved, pending implementation plan

## Why

AltMount reimplements PAR2's conventions that `github.com/javi11/gopar-turbo`
already implements. On 2026-08-25 that duplication produced a live repair
failure: every recovered slice of a 4.7 GB release failed its IFSC MD5
verification.

The cause was a FileID comparator. PAR2 orders FileIDs with byte 15 most
significant — par2cmdline's `MD5Hash::operator<`, faithfully mirrored in
gopar-turbo's `par2/main_packet.go` as `fileIDLess`. AltMount's parser used
`bytes.Compare`, which is byte 0 most significant. The recovery-set file order
defines the global input slice numbering, which selects each slice's
Vandermonde constant, so a permuted order hands the solver the wrong
coefficient for every slice.

The failure was invisible to every other check in the pipeline. Present slices
still passed their IFSC CRC32 (a file-local index, unaffected by global
numbering) and recovery payloads still passed their own packet MD5. Only the
final MD5 check on reconstructed slices caught it.

Two independent test blind spots let it survive:

1. `internal/testsupport/par2gen` used the same wrong comparator, so it wrote
   Main packets lexicographically *and* numbered slices to match. Every
   generator-based test agreed with the bug.
2. Every real-par2cmdline test used `idx.Recovery[0]`, whose exponent is 0.
   With `g^0 = 1` all coefficients collapse to 1, the equation degenerates to
   a plain XOR of all slices, and the result stops depending on slice
   numbering. The external fixture literally could not detect the bug.

The comparator is fixed (see "Relationship to the bug fix" below). This design
addresses the structural cause: PAR2's conventions live in two places and can
drift again.

## Goal and non-goals

**Goal.** Exactly one implementation of PAR2's conventions and reconstruction
arithmetic, owned by gopar-turbo, tested there against real par2cmdline
fixtures. AltMount keeps everything usenet-specific.

**Non-goals.**

- Performance. Both sides already call the same `gf16` SIMD kernels; AltMount
  imports them directly today and `Accelerated()` is true on supported
  platforms. This change is bit-for-bit output-identical and speed-neutral.
  Measured for context: the fold on the failing release is ~10 s of GF work
  against ~85 s of network transfer, and AltMount already overlaps the two.
- Moving the packet parser. AltMount's parser does three things gopar-turbo's
  path-based decoder cannot: streams over `io.Reader` with seek-skip so
  recovery payloads are located but never downloaded, resynchronises past
  damage and drops packets on MD5 mismatch, and records `BodyOffset` for lazy
  fetch. It stays.
- Adopting `rsec16` for the fold. `rsec16.FoldInputs` is pull-based over a
  fixed matrix, so it cannot express `AddMissing` — discovering an unknown
  mid-fold, which is how AltMount absorbs articles that die between planning
  and the sweep. `rsec16.applyMatrixGF16` also prepares all inputs up front,
  assuming every input is addressable; AltMount's inputs arrive one at a time
  from the network. Its byte-range fold split is tuned for that.

## Architecture

A new package `par2stream` in gopar-turbo, alongside `par2` (path-based
verify/repair) and `rsec16` (RS primitives). It owns three concerns.

### 1. Conventions

The single source of truth for the rules that caused the bug.

```go
// FileIDLess orders FileIDs the way PAR2 does: byte 15 most significant.
// NOT bytes.Compare.
func FileIDLess(a, b [16]byte) bool

// VandermondeBase returns the GF(2^16) constant for global input slice j:
// 2^i over the sequence of i not divisible by 3, 5, 17 or 257.
func VandermondeBase(j int) gf2p16.T
```

### 2. Geometry

Global slice numbering derived from file lengths in Main-packet order.

```go
func NewGeometry(sliceSize int64, lengths []uint64) (*Geometry, error)

func (g *Geometry) SliceSize() int64
func (g *Geometry) TotalSlices() int
func (g *Geometry) FileStartSlice(fileIdx int) int
func (g *Geometry) SliceCount(fileIdx int) int
func (g *Geometry) FileForSlice(global int) (fileIdx, local int)

// SlicesForRange returns the inclusive global slice span covering
// [off, off+size) within file fileIdx.
func (g *Geometry) SlicesForRange(fileIdx int, off, size int64) (first, last int)
```

`SlicesForRange` replaces an expression currently written out four times in
`internal/par2repair/job.go` (`absorbDead`, `patchCorruptArticle`,
`emitPatches`) plus once in `planner.go` — precisely the kind of repetition
that lets a convention drift.

### 3. Solver, verification and arena

Lifted from AltMount essentially unchanged; the exported shape already matches.

```go
type BufAlloc func(n int) ([]byte, error)

type Solver struct{ /* ... */ }

func NewSolver(missing []int, exps []uint32, sliceSize int) (*Solver, error)
func NewSolverAlloc(missing []int, exps []uint32, sliceSize int, alloc BufAlloc) (*Solver, error)
func AccumulatorArenaBytes(sliceSize int) (int64, error)

func (s *Solver) AddRecovery(i int, payload []byte) error
func (s *Solver) SeedRecoveryOwning(i int, payload []byte) error
func (s *Solver) AddMissing(global int) error
func (s *Solver) FoldPresent(global int, slice []byte)
func (s *Solver) Solve() ([][]byte, error)
func (s *Solver) VerifyHeldOutRow(recovered [][]byte) (row int, checked, ok bool)
func (s *Solver) Close()

var ErrSingularMatrix error

// Disk-backed allocator for solves over the memory budget.
type Arena struct{ /* ... */ }
func NewArena(dir string, capacity int64) (*Arena, error)
func (a *Arena) Alloc(n int) ([]byte, error)
func (a *Arena) Close() error
```

Verification primitives, taking checksums as plain values so upstream needs no
dependency on AltMount's parser:

```go
type SliceCheck struct {
	MD5   [16]byte
	CRC32 uint32
}

// perFile is indexed by file, then by that file's local slice index.
func NewChecksumSet(g *Geometry, perFile [][]SliceCheck) (*ChecksumSet, error)
func (c *ChecksumSet) At(global int) SliceCheck

func VerifyPresent(slice []byte, want SliceCheck) bool

type Mismatch struct {
	Global   int
	FileIdx  int
	Local    int
}

// VerifyRecovered returns every recovered slice failing its IFSC MD5,
// ascending by global index.
func VerifyRecovered(g *Geometry, c *ChecksumSet, recovered [][]byte, missing []int) []Mismatch
```

`VerifyRecovered` returns all mismatches rather than the first. The original
returned early while iterating a map, so the reported slice number was
arbitrary — which made the production error message impossible to reason about.

Checksums are copied rather than accessed through an interface. At 20 bytes per
slice that is ~40 KB for a 1,965-slice release: negligible, and it keeps
upstream testable without a fake.

AltMount's parser keeps its own structurally identical `par2.SliceCheck`, so
building a `ChecksumSet` needs an explicit conversion at the call site — one
loop over `idx.SliceChecks[fileID]` per recovery-set member, in
`RecoveryIDs` order. AltMount's type is deliberately not replaced by an alias:
the parser must stay independent of the engine so it can be tested and reasoned
about alone.

## Data flow

AltMount's parser becomes purely byte-level — it reports what it read without
interpreting order.

```
par2.ParseIndex
  └─ Index{ RecoveryIDs (Main-packet stored order), Files, SliceChecks, Recovery }
       ├─ lengths in RecoveryIDs order  ──→ par2stream.NewGeometry
       ├─ SliceChecks per file          ──→ par2stream.NewChecksumSet
       └─ recovery exponents            ──→ par2stream.NewSolverAlloc
                                              │
AltMount sweep: fetch, prefetch, absorb,      │
                replan, spare swapping  ──────┤
                                              ↓
                                    Solve → VerifyRecovered
                                              ↓
                          AltMount: emitPatches → PatchStore
```

The sweep loop stays in AltMount because it is entangled with AltMount policy:
the article prefetch pipeline, `absorbDead` (which reasons about articles, not
slices), margin-row and spare-row policy, replan budgets, and patch emission.
Only the checks themselves are generic.

## What AltMount removes

| Location | Lines |
| --- | --- |
| `internal/par2repair/solver.go` | 433 |
| `internal/par2repair/arena.go`, `mmap_unix.go`, `mmap_windows.go` | 124 |
| slice math in `internal/par2repair/planner.go` | ~40 |
| verify and `first`/`last` math in `internal/par2repair/job.go` | ~80 |
| `fileIDLess` in `internal/importer/parser/par2/index.go` and its `par2gen` copy | ~25 |
| **Total** | **~700** |

`internal/par2repair/solver_test.go` (~420 lines) moves upstream with the code.

Unchanged: `resolve.go`, `resolve_nzb.go`, `service.go`, `patchstore.go`,
`fetch.go`, `job.go`'s sweep, and the whole `internal/importer/parser/par2`
package apart from the comparator.

## Testing

The justification for this refactor is test integrity, so the test plan is the
substance, not an afterthought.

**Upstream owns the convention tests.** Move the `ordercheck` fixture — ten
real par2cmdline files whose lexicographic and PAR2 orderings provably differ,
verified to fail with the bug present and pass with it fixed — into gopar-turbo
along with the ordering and global-numbering assertions.

**Non-zero exponents are mandatory.** Any test reconstructing from a real
recovery payload must use a row with a non-zero exponent, enforced by a helper
that fails rather than silently picking exponent 0. This is the rule that would
have caught the original bug.

**Multi-file fixtures are mandatory for ordering tests.** With two members the
two orderings coincide about half the time, so two-file fixtures cannot detect a
comparator error. Ordering tests need enough members to diverge.

**A second oracle inside one repo.** gopar-turbo's existing `par2` package has
its own independent Main-packet reader and `fileIDLess`. A test asserting
`par2stream.FileIDLess` agrees with it gives genuine cross-implementation
validation with no self-reference — the property AltMount's own fixtures could
never have.

**`par2gen` stops being self-referential.** It drops its comparator copy and
calls upstream, so a generated fixture can no longer agree with a convention
bug.

**AltMount keeps integration coverage.** `job_test.go`, `service_test.go`,
`resolve_test.go` and the resolve/NZB tests continue to run against the
upstream engine, including the mid-sweep absorb, corrupt-replan and
spill-to-disk cases.

**Awkward slice sizes stay covered.** Real releases use sizes like 2380956
(a multiple of 4 but of no higher power of two). Upstream keeps round-trip
tests at such sizes and at sizes above the 128 KiB parallel-fold threshold, so
the SIMD prepared layout and stride-aligned chunk split are exercised at
shipping dimensions.

## Sequencing

1. **Ship the comparator fix on its own, first.** It resolves a live failure
   and must not wait on a two-repo refactor.
2. Add `par2stream` upstream with its tests and fixtures; tag a release.
3. Point AltMount at it with a `go.mod` `replace` during development, run the
   full suite, then bump the dependency and drop the replace.

## Risks and accepted trade-offs

- **Slower iteration.** Every upstream change needs a version bump to reach
  AltMount. Mitigated during development by the `replace` directive.
- **No performance gain.** Same kernels; this buys correctness and ~700 fewer
  lines. Worth stating plainly so it is not mistaken for an optimisation.
- **Packet parsing stays duplicated in spirit.** The parser remains in
  AltMount, so a packet-level divergence is still possible. Only the
  conventions are unified. Accepted deliberately: the streaming, seek-skip and
  damage-tolerance behaviour has no upstream counterpart, and reworking
  gopar-turbo's decoder to acquire it would risk its existing users.
- **A conversion at the boundary.** FileIDs are `[16]byte` on both sides so
  they pass straight through, but `SliceCheck` is declared in both packages and
  needs an explicit copy loop. Deliberate: keeping the parser independent of the
  engine is worth a few lines of adaptation.

## Relationship to the bug fix

The comparator fix is already implemented in AltMount and is independent of
this design:

- `internal/importer/parser/par2/index.go` preserves the Main packet's stored
  order instead of re-sorting it, and documents `fileIDLess`.
- `internal/testsupport/par2gen` emits PAR2-conventional order.
- New fixture `testdata/ordercheck` plus ordering and multi-file solve tests.
- All real-tool solves use non-zero exponents via `nonZeroExponentRef`.
- Diagnostics on verification failure: all mismatches, file names, article-size
  provenance, dead-article adjacency, `gf16.Accelerated()`,
  `MainIDsWereSorted`, and a held-out-row check separating "inputs or numbering
  wrong" from "fold or solve wrong".
- `Solver.VerifyHeldOutRow`, with a test proving it accepts a correct solve and
  rejects one built on swapped slice indices.

This design moves that fixed code upstream. It does not change its behaviour.
