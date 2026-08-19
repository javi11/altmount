package par2repair

import (
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"hash/crc32"
	"log/slog"
	"slices"
	"sort"

	"github.com/javi11/nntppool/v4"

	"github.com/javi11/altmount/internal/importer/parser/par2"
)

// ArticleFetcher fetches one decoded article payload. Implementations must be
// safe for concurrent use; the job keeps several fetches in flight.
type ArticleFetcher interface {
	Fetch(ctx context.Context, messageID string) ([]byte, error)
}

// fetchAhead bounds how many article fetches a job keeps in flight (and how
// many fetched-but-unconsumed payloads it buffers, so memory stays at
// fetchAhead x article size). Fetches also pass through the pool's import
// connection budget, which is the real global throttle.
const fetchAhead = 8

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

// JobProgress receives sweep progress: articles processed so far out of the
// total the sweep covers. Called from the job goroutine; must be fast.
type JobProgress func(doneArticles, totalArticles int)

// JobOption customizes RunJob.
type JobOption func(*jobOptions)

type jobOptions struct {
	progress JobProgress
}

// WithProgress reports sweep progress through cb. A re-sweep (singular-matrix
// or corrupt-slice replan) restarts the count.
func WithProgress(cb JobProgress) JobOption {
	return func(o *jobOptions) { o.progress = cb }
}

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
	opts ...JobOption,
) error {
	var o jobOptions
	for _, opt := range opts {
		opt(&o)
	}
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
	// Refs are visited in (file, offset) order so their backing articles
	// stream through a bounded prefetch window instead of piling up on the
	// heap; on a spill plan the payloads themselves land in a disk arena.
	refs := slices.Clone(plan.Recovery)
	spares := slices.Clone(plan.SpareRecovery)
	par2Cache := newArticleCache(resolveCacheCap)

	scratch := store.ScratchDir()
	var payloadArena *arena
	persist := func(data []byte) ([]byte, error) {
		if payloadArena == nil {
			return data, nil
		}
		buf, err := payloadArena.alloc(len(data))
		if err != nil {
			return nil, err
		}
		copy(buf, data)
		return buf, nil
	}
	if plan.SpillToDisk {
		// Every chosen slice plus every spare that could ever be swapped in.
		a, err := newArena(scratch, int64(len(refs)+len(spares))*sliceSize)
		if err != nil {
			return err
		}
		payloadArena = a
		defer func() { _ = payloadArena.Close() }()
	}

	order := make([]int, k)
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(x, y int) bool {
		a, b := refs[order[x]], refs[order[y]]
		if a.FileIndex != b.FileIndex {
			return a.FileIndex < b.FileIndex
		}
		return a.BodyOffset < b.BodyOffset
	})

	payloads := make([][]byte, k)
	feed := newArticleFeed(ctx, fetch, recoveryArticleIDs(par2Files, refs, order, sliceSize))
	defer feed.stop()
	for _, i := range order {
		for {
			ref := refs[i]
			data, err := readRangeFrom(feed.get, par2Files[ref.FileIndex], ref.BodyOffset, sliceSize)
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
			if payloads[i], err = persist(data); err != nil {
				return err
			}
			break
		}
	}
	feed.stop()

	totalArticles := 0
	for _, f := range plan.Files {
		totalArticles += len(f.Articles)
	}

	// Attempt loop: a singular matrix (a known PAR2 Vandermonde flaw) retries
	// with a spare recovery slice, and a corrupt present slice is reclassified
	// as missing and solved for with one more recovery slice — both at the
	// cost of a fresh sweep.
	missing := slices.Clone(plan.Missing)
	corruptReplans := 0
	// On a spill plan each attempt gets a fresh disk arena for its
	// accumulators and recovered slices (fresh file pages arrive zeroed, so a
	// re-sweep never sees a stale accumulator).
	var attemptArena *arena
	defer func() {
		if attemptArena != nil {
			_ = attemptArena.Close()
		}
	}()
	for {
		if attemptArena != nil {
			_ = attemptArena.Close()
			attemptArena = nil
		}
		alloc := heapAlloc
		if plan.SpillToDisk {
			a, err := newArena(scratch, 2*int64(len(missing))*sliceSize)
			if err != nil {
				return err
			}
			attemptArena = a
			alloc = attemptArena.alloc
		}

		exps := make([]uint32, len(refs))
		for i, r := range refs {
			exps[i] = r.Exponent
		}
		solver, err := NewSolverAlloc(missing, exps, plan.SliceSize, alloc)
		if err != nil {
			return err
		}
		for i, p := range payloads {
			solver.AddRecovery(i, p)
		}

		doneArticles := 0
		onArticle := func() {
			doneArticles++
			if o.progress != nil {
				o.progress(doneArticles, totalArticles)
			}
		}
		if err := sweep(ctx, plan, idx, fetch, solver, startSlice, missingPos, onArticle, log); err != nil {
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
			if data, ferr = persist(data); ferr != nil {
				return ferr
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
			if data, ferr = persist(data); ferr != nil {
				return ferr
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
	onArticle func(),
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

		slots, stop := prefetchArticles(ctx, fetch, f.Articles)
		for _, a := range f.Articles {
			if err := ctx.Err(); err != nil {
				stop()
				return err
			}
			if onArticle != nil {
				onArticle()
			}
			if a.Dead {
				// Zero-advance: the affected slices are in the missing set and
				// will be skipped by completeSlice.
				if err := feed(make([]byte, a.Size)); err != nil {
					stop()
					return err
				}
				continue
			}
			slot, ok := <-slots
			if !ok {
				// The prefetcher only closes early when the context ended.
				stop()
				return ctx.Err()
			}
			res := <-slot
			if res.err != nil {
				stop()
				if errors.Is(res.err, nntppool.ErrArticleNotFound) {
					// An article that died between planning and sweeping needs
					// a replan: surface a typed error so the service persists
					// the discovery and the retry's plan includes it in the
					// missing set.
					return &SweepDeadArticleError{MessageID: a.MessageID, Err: res.err}
				}
				return fmt.Errorf("par2repair: fetch article %s: %w", a.MessageID, res.err)
			}
			if int64(len(res.data)) != a.Size {
				stop()
				return fmt.Errorf("%w: article %s decoded to %d bytes, expected %d", ErrUnrepairable, a.MessageID, len(res.data), a.Size)
			}
			if err := feed(res.data); err != nil {
				stop()
				return err
			}
		}
		stop()
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

// recoveryArticleIDs lists, in consumption order, every distinct article
// backing the recovery slices refs, visited in the given order.
func recoveryArticleIDs(par2Files []SetFile, refs []par2.RecoverySliceRef, order []int, sliceSize int64) []string {
	seen := map[string]bool{}
	var ids []string
	for _, i := range order {
		ref := refs[i]
		f := par2Files[ref.FileIndex]
		var artStart int64
		for _, a := range f.Articles {
			artEnd := artStart + a.Size
			if artEnd > ref.BodyOffset && artStart < ref.BodyOffset+sliceSize && !seen[a.MessageID] {
				seen[a.MessageID] = true
				ids = append(ids, a.MessageID)
			}
			artStart = artEnd
			if artStart >= ref.BodyOffset+sliceSize {
				break
			}
		}
	}
	return ids
}

// feedRetain bounds how many consumed articles a feed keeps cached: enough
// for payloads straddling article boundaries and adjacent refs sharing a
// boundary article, without growing with the recovery set.
const feedRetain = 8

// articleFeed streams a known-in-advance article sequence through the bounded
// prefetch pipeline. get serves prefetched payloads for upcoming sequence ids
// and falls back to a direct fetch for ids outside the sequence (spare
// recovery slices swapped in after planning). Not safe for concurrent use.
type articleFeed struct {
	ctx    context.Context
	fetch  ArticleFetcher
	ids    []string
	posOf  map[string]int
	next   int
	have   map[string][]byte
	order  []string
	slots  <-chan chan fetchResult
	cancel func()
}

func newArticleFeed(ctx context.Context, fetch ArticleFetcher, ids []string) *articleFeed {
	arts := make([]Article, len(ids))
	posOf := make(map[string]int, len(ids))
	for i, id := range ids {
		arts[i] = Article{MessageID: id}
		posOf[id] = i
	}
	slots, cancel := prefetchArticles(ctx, fetch, arts)
	return &articleFeed{
		ctx: ctx, fetch: fetch, ids: ids, posOf: posOf,
		have: map[string][]byte{}, slots: slots, cancel: cancel,
	}
}

// get returns one article's payload, popping the prefetch pipeline forward to
// the article's position in the sequence.
func (f *articleFeed) get(id string) ([]byte, error) {
	if data, ok := f.have[id]; ok {
		return data, nil
	}
	pos, ok := f.posOf[id]
	if !ok || pos < f.next {
		// Outside the planned sequence (a spare swap) or already evicted.
		return f.fetch.Fetch(f.ctx, id)
	}
	for f.next <= pos {
		slot, ok := <-f.slots
		if !ok {
			// The prefetcher only closes early when the context ended.
			if err := f.ctx.Err(); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("par2repair: article feed exhausted before %s", id)
		}
		res := <-slot
		cur := f.ids[f.next]
		f.next++
		if res.err != nil {
			if cur == id {
				return nil, res.err
			}
			continue // an id the caller ended up not needing (spare swap)
		}
		f.retain(cur, res.data)
	}
	return f.have[id], nil
}

func (f *articleFeed) retain(id string, data []byte) {
	f.have[id] = data
	f.order = append(f.order, id)
	for len(f.order) > feedRetain {
		delete(f.have, f.order[0])
		f.order = f.order[1:]
	}
}

// stop cancels outstanding prefetches. Idempotent.
func (f *articleFeed) stop() { f.cancel() }

// fetchResult is one prefetched article payload (or its fetch error).
type fetchResult struct {
	data []byte
	err  error
}

// prefetchArticles pipelines fetches of the live articles in arts, preserving
// order: the returned channel yields one single-use result channel per live
// article, in article order. At most fetchAhead fetches are in flight or
// buffered at once, so memory stays bounded while the network stays busy.
// stop cancels outstanding fetches; callers must call it once done (early
// return or normal completion).
func prefetchArticles(ctx context.Context, fetch ArticleFetcher, arts []Article) (slots <-chan chan fetchResult, stop func()) {
	pctx, cancel := context.WithCancel(ctx)
	out := make(chan chan fetchResult, fetchAhead)
	go func() {
		defer close(out)
		for _, a := range arts {
			if a.Dead {
				continue
			}
			slot := make(chan fetchResult, 1)
			go func(id string) {
				data, err := fetch.Fetch(pctx, id)
				slot <- fetchResult{data: data, err: err}
			}(a.MessageID)
			select {
			case out <- slot:
			case <-pctx.Done():
				return
			}
		}
	}()
	return out, cancel
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
func readRange(ctx context.Context, fetch ArticleFetcher, f SetFile, off, n int64, cache *articleCache) ([]byte, error) {
	return readRangeFrom(func(id string) ([]byte, error) {
		return fetchCached(ctx, fetch, id, cache)
	}, f, off, n)
}

// readRangeFrom reads [off, off+n) of a file's concatenated article payloads
// through get (typically an articleFeed).
func readRangeFrom(get func(string) ([]byte, error), f SetFile, off, n int64) ([]byte, error) {
	out := make([]byte, n)
	var artStart int64
	for _, a := range f.Articles {
		artEnd := artStart + a.Size
		if artEnd > off && artStart < off+n {
			data, err := get(a.MessageID)
			if err != nil {
				return nil, err
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
