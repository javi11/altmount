package par2repair

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"slices"

	"github.com/javi11/nntppool/v4"

	"github.com/javi11/altmount/internal/importer/parser/par2"
)

// ArticleFetcher fetches one decoded article payload. Implementations must be
// safe for sequential reuse; the job calls it from a single goroutine.
type ArticleFetcher interface {
	Fetch(ctx context.Context, messageID string) ([]byte, error)
}

// SweepDeadArticleError reports an article that was live at planning time but
// vanished during the sweep. Transient at the job level (a retry can replan),
// but the caller should persist the discovery so the next plan includes it in
// the missing set instead of hitting the same wall.
type SweepDeadArticleError struct {
	MessageID string
	Err       error
}

func (e *SweepDeadArticleError) Error() string {
	return fmt.Sprintf("par2repair: article %s vanished during sweep: %v", e.MessageID, e.Err)
}

func (e *SweepDeadArticleError) Unwrap() error { return e.Err }

// maxCorruptReplans bounds how many corrupt present slices a single job may
// reclassify as missing. Each reclassification grows the solver by one
// accumulator (slice-size bytes) and costs a fresh sweep, so the bound caps
// both memory growth and re-sweep count.
const maxCorruptReplans = 8

// corruptSliceError signals a present slice whose CRC32 didn't match its IFSC
// checksum during the sweep. Unlike SweepDeadArticleError (article vanished),
// the fetch succeeded but the data is bad: the attempt loop reclassifies the
// slice as missing and retries with one more recovery slice.
type corruptSliceError struct {
	global int
	fileID [16]byte
	local  int
}

func (e *corruptSliceError) Error() string {
	return fmt.Sprintf("par2repair: file %x slice %d (global %d) failed CRC32 verification", e.fileID, e.local, e.global)
}

// RunJob executes a repair plan: fetches the chosen recovery slices, sweeps
// every present input slice of the recovery set (verifying each against its
// IFSC CRC32), solves for the missing slices, verifies them against their
// IFSC MD5s and writes the dead articles' payloads to the patch store.
//
// par2Files describes the usenet articles of each PAR2 file, positionally
// matching the streams given to par2.ParseIndex (so RecoverySliceRef.FileIndex
// indexes into it).
//
// Errors wrapping ErrUnrepairable are permanent; anything else is transient
// and worth retrying later.
func RunJob(
	ctx context.Context,
	plan *Plan,
	idx *par2.Index,
	par2Files []SetFile,
	fetch ArticleFetcher,
	store *PatchStore,
	log *slog.Logger,
) error {
	sliceSize := int64(plan.SliceSize)
	k := len(plan.Missing)

	// Global slice bookkeeping: start slice per file and missing positions.
	startSlice := make([]int64, len(plan.Files))
	var total int64
	for i, f := range plan.Files {
		startSlice[i] = total
		total += (int64(f.Length) + sliceSize - 1) / sliceSize
	}
	missingPos := make(map[int]int, k) // global slice index -> position in plan.Missing
	for i, m := range plan.Missing {
		missingPos[m] = i
	}

	// Resolve recovery payloads, swapping in spares for dead par2 articles.
	refs := slices.Clone(plan.Recovery)
	spares := slices.Clone(plan.SpareRecovery)
	par2Cache := map[string][]byte{}
	payloads := make([][]byte, k)
	for i := 0; i < k; {
		ref := refs[i]
		data, err := readRange(ctx, fetch, par2Files[ref.FileIndex], ref.BodyOffset, sliceSize, par2Cache)
		if err != nil {
			if errors.Is(err, nntppool.ErrArticleNotFound) {
				if len(spares) == 0 {
					return fmt.Errorf("%w: recovery slice exponent %d is unreachable and no spares remain", ErrUnrepairable, ref.Exponent)
				}
				log.WarnContext(ctx, "recovery slice unreachable, swapping in spare",
					"exponent", ref.Exponent, "spare_exponent", spares[0].Exponent)
				refs[i] = spares[0]
				spares = spares[1:]
				continue
			}
			return fmt.Errorf("par2repair: fetch recovery slice exponent %d: %w", ref.Exponent, err)
		}
		payloads[i] = data
		i++
	}

	// Attempt loop: a singular matrix (a known PAR2 Vandermonde flaw) retries
	// with a spare recovery slice, and a corrupt present slice is reclassified
	// as missing and solved for with one more recovery slice — both at the
	// cost of a fresh sweep.
	missing := slices.Clone(plan.Missing)
	corruptReplans := 0
	for {
		exps := make([]uint32, len(refs))
		for i, r := range refs {
			exps[i] = r.Exponent
		}
		solver, err := NewSolver(missing, exps, plan.SliceSize)
		if err != nil {
			return err
		}
		for i, p := range payloads {
			solver.AddRecovery(i, p)
		}

		if err := sweep(ctx, plan, idx, fetch, solver, startSlice, missingPos, log); err != nil {
			var corrupt *corruptSliceError
			if !errors.As(err, &corrupt) {
				return err
			}
			// A present-but-corrupt slice is just another unknown: grow the
			// missing set by it and take one more recovery slice.
			corruptReplans++
			if corruptReplans > maxCorruptReplans {
				return fmt.Errorf("%w: %v and the corrupt-slice replan budget (%d) is exhausted",
					ErrUnrepairable, corrupt, maxCorruptReplans)
			}
			if len(spares) == 0 {
				return fmt.Errorf("%w: %v and no spare recovery slices remain", ErrUnrepairable, corrupt)
			}
			log.WarnContext(ctx, "present slice failed CRC32, reclassifying as missing",
				"global_slice", corrupt.global, "spare_exponent", spares[0].Exponent)
			missing = append(missing, corrupt.global)
			missingPos[corrupt.global] = len(missing) - 1
			refs = append(refs, spares[0])
			spares = spares[1:]
			extra := refs[len(refs)-1]
			data, ferr := readRange(ctx, fetch, par2Files[extra.FileIndex], extra.BodyOffset, sliceSize, par2Cache)
			if ferr != nil {
				return fmt.Errorf("%w: spare recovery slice unreachable: %v", ErrUnrepairable, ferr)
			}
			payloads = append(payloads, data)
			continue
		}

		recovered, err := solver.Solve()
		if errors.Is(err, ErrSingularMatrix) {
			if len(spares) == 0 {
				return fmt.Errorf("%w: recovery matrix singular and no spare recovery slices remain", ErrUnrepairable)
			}
			swap := len(refs) - 1
			log.WarnContext(ctx, "recovery matrix singular, retrying with spare recovery slice",
				"dropped_exponent", refs[swap].Exponent, "spare_exponent", spares[0].Exponent)
			refs[swap] = spares[0]
			spares = spares[1:]
			data, ferr := readRange(ctx, fetch, par2Files[refs[swap].FileIndex], refs[swap].BodyOffset, sliceSize, par2Cache)
			if ferr != nil {
				return fmt.Errorf("%w: spare recovery slice unreachable: %v", ErrUnrepairable, ferr)
			}
			payloads[swap] = data
			continue
		}
		if err != nil {
			return err
		}

		// Verify every recovered slice against its IFSC MD5 before storing.
		if err := verifyRecovered(plan, idx, recovered, startSlice, missingPos); err != nil {
			return err
		}

		return emitPatches(plan, recovered, sliceSize, startSlice, missingPos, store, log)
	}
}

// sweep streams every article of the recovery set in order, assembles input
// slices, verifies present slices against IFSC CRC32 and folds them into the
// solver. Slices touched by dead articles are skipped (they are the unknowns).
func sweep(
	ctx context.Context,
	plan *Plan,
	idx *par2.Index,
	fetch ArticleFetcher,
	solver *Solver,
	startSlice []int64,
	missingPos map[int]int,
	log *slog.Logger,
) error {
	sliceSize := plan.SliceSize
	for fi, f := range plan.Files {
		checks := idx.SliceChecks[f.FileID]
		buf := make([]byte, sliceSize)
		fill := 0
		local := 0

		completeSlice := func() error {
			global := int(startSlice[fi]) + local
			if _, isMissing := missingPos[global]; !isMissing {
				if local < len(checks) && crc32.ChecksumIEEE(buf) != checks[local].CRC32 {
					// Present but corrupt: signal the attempt loop to
					// reclassify this slice as missing and re-sweep.
					return &corruptSliceError{global: global, fileID: f.FileID, local: local}
				}
				solver.FoldPresent(global, buf)
			}
			local++
			fill = 0
			for i := range buf {
				buf[i] = 0
			}
			return nil
		}

		feed := func(data []byte) error {
			for len(data) > 0 {
				n := copy(buf[fill:], data)
				fill += n
				data = data[n:]
				if fill == sliceSize {
					if err := completeSlice(); err != nil {
						return err
					}
				}
			}
			return nil
		}

		for _, a := range f.Articles {
			if err := ctx.Err(); err != nil {
				return err
			}
			if a.Dead {
				// Zero-advance: the affected slices are in the missing set and
				// will be skipped by completeSlice.
				if err := feed(make([]byte, a.Size)); err != nil {
					return err
				}
				continue
			}
			data, err := fetch.Fetch(ctx, a.MessageID)
			if err != nil {
				if errors.Is(err, nntppool.ErrArticleNotFound) {
					// An article that died between planning and sweeping needs
					// a replan: surface a typed error so the service persists
					// the discovery and the retry's plan includes it in the
					// missing set.
					return &SweepDeadArticleError{MessageID: a.MessageID, Err: err}
				}
				return fmt.Errorf("par2repair: fetch article %s: %w", a.MessageID, err)
			}
			if int64(len(data)) != a.Size {
				return fmt.Errorf("%w: article %s decoded to %d bytes, expected %d", ErrUnrepairable, a.MessageID, len(data), a.Size)
			}
			if err := feed(data); err != nil {
				return err
			}
		}
		// Final partial slice: buf is already zero-padded past fill.
		if fill > 0 {
			if err := completeSlice(); err != nil {
				return err
			}
		}
		log.DebugContext(ctx, "swept recovery-set file", "file_index", fi, "slices", local)
	}
	return nil
}

// verifyRecovered checks every recovered slice against its IFSC MD5.
func verifyRecovered(plan *Plan, idx *par2.Index, recovered [][]byte, startSlice []int64, missingPos map[int]int) error {
	for global, pos := range missingPos {
		fi := fileForSlice(startSlice, global)
		local := global - int(startSlice[fi])
		checks := idx.SliceChecks[plan.Files[fi].FileID]
		if local >= len(checks) {
			return fmt.Errorf("%w: recovered slice %d outside IFSC range", ErrUnrepairable, global)
		}
		if md5.Sum(recovered[pos]) != checks[local].MD5 {
			return fmt.Errorf("%w: recovered slice %d failed MD5 verification", ErrUnrepairable, global)
		}
	}
	return nil
}

// emitPatches cuts recovered slices back into dead-article payloads and
// stores them atomically.
//
// Only plan.DeadArticles get patches. Corrupt-but-present slices reclassified
// during the job are solved for (as unknowns) but deliberately NOT patched:
// their articles are still served fine by providers, and the read path only
// consults the patch store when an article is MISSING — a patch for a present
// article would never be read.
func emitPatches(plan *Plan, recovered [][]byte, sliceSize int64, startSlice []int64, missingPos map[int]int, store *PatchStore, log *slog.Logger) error {
	for _, da := range plan.DeadArticles {
		payload := make([]byte, da.Size)
		first := startSlice[da.FileIdx] + da.FileStart/sliceSize
		last := startSlice[da.FileIdx] + (da.FileStart+da.Size-1)/sliceSize
		for g := first; g <= last; g++ {
			pos, ok := missingPos[int(g)]
			if !ok {
				return fmt.Errorf("par2repair: internal error: slice %d for article %s not in missing set", g, da.MessageID)
			}
			sliceFileStart := (g - startSlice[da.FileIdx]) * sliceSize
			// Overlap of [sliceFileStart, sliceFileStart+sliceSize) with the
			// article's [da.FileStart, da.FileStart+da.Size).
			from := max(sliceFileStart, da.FileStart)
			to := min(sliceFileStart+sliceSize, da.FileStart+da.Size)
			copy(payload[from-da.FileStart:to-da.FileStart], recovered[pos][from-sliceFileStart:to-sliceFileStart])
		}
		if err := store.Put(da.MessageID, payload); err != nil {
			return fmt.Errorf("par2repair: store patch for %s: %w", da.MessageID, err)
		}
		log.InfoContext(context.Background(), "stored repaired article payload",
			"message_id", da.MessageID, "bytes", len(payload))
	}
	return nil
}

// fileForSlice returns the index of the plan file containing the global slice.
func fileForSlice(startSlice []int64, global int) int {
	for i := len(startSlice) - 1; i >= 0; i-- {
		if int64(global) >= startSlice[i] {
			return i
		}
	}
	return 0
}

// readRange reads [off, off+n) from a file's concatenated article payloads,
// fetching only the overlapping articles (with a per-job cache).
func readRange(ctx context.Context, fetch ArticleFetcher, f SetFile, off, n int64, cache map[string][]byte) ([]byte, error) {
	out := make([]byte, n)
	var artStart int64
	for _, a := range f.Articles {
		artEnd := artStart + a.Size
		if artEnd > off && artStart < off+n {
			data, ok := cache[a.MessageID]
			if !ok {
				var err error
				data, err = fetch.Fetch(ctx, a.MessageID)
				if err != nil {
					return nil, err
				}
				cache[a.MessageID] = data
			}
			from := max(off, artStart)
			to := min(off+n, artEnd)
			copy(out[from-off:to-off], data[from-artStart:to-artStart])
		}
		artStart = artEnd
		if artStart >= off+n {
			break
		}
	}
	return out, nil
}
