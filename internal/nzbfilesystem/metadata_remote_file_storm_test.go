package nzbfilesystem

import (
	"context"
	"io"
	"math/rand"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/goroutines"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// slowCloseReader is a stand-in io.ReadCloser whose Close blocks for the
// configured duration. It models a UsenetReader.Close that is waiting for
// in-flight BodyPriority calls to drain. Each Close increments a
// completion counter so tests can observe how many closers actually ran.
type slowCloseReader struct {
	closeDelay   time.Duration
	closes       atomic.Int32
	closedSignal chan struct{}
}

func newSlowCloseReader(delay time.Duration) *slowCloseReader {
	return &slowCloseReader{
		closeDelay:   delay,
		closedSignal: make(chan struct{}),
	}
}

func (r *slowCloseReader) Read(_ []byte) (int, error) {
	return 0, io.EOF
}

func (r *slowCloseReader) Close() error {
	time.Sleep(r.closeDelay)
	r.closes.Add(1)
	select {
	case <-r.closedSignal:
	default:
	}
	return nil
}

// metadata_remote_file_storm_test.go reproduces the storm conditions
// caused by MetadataVirtualFile's per-request reader lifecycle. Each test
// uses the harness in streamtest_helpers_test.go to construct a minimal
// MetadataVirtualFile wired to the fake pool, then drives a ReadAt or
// Seek workload designed to expose the storm.
//
// Like the usenet-level storm tests, each test pins the CURRENT bad
// behavior with a concrete assertion. When the fix lands, the assertion
// fails and must be inverted to enforce the TARGET invariant.

// TestStorm_RandomReadAtCreatesEphemeralReaderPerCall pins the post-S5
// invariant: ephemeral ReadAts with locality (the realistic Plex/
// Jellyfin scrubbing pattern — many small reads within a small
// segment window) are coalesced by the per-file random-read LRU cache
// so total wire calls stay near the unique-segment count rather than
// the read count.
//
// Workload: 200 small ReadAts spread randomly across 8 distinct
// segments (fits in randomReadCacheSize). Without the cache, every
// call would fetch its containing segment → ≈200 BodyPriority calls.
// With the cache, after each segment is fetched once, subsequent reads
// in the same segment hit the cache → ≈8 BodyPriority calls.
func TestStorm_RandomReadAtCreatesEphemeralReaderPerCall(t *testing.T) {
	t.Parallel()
	const (
		segCount     = 200
		segSize      = 1024
		readCount    = 200
		readSize     = 64
		hotWindowSegs = 8 // working set fits in randomReadCacheSize
		maxPrefetch  = 4
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	configurePoolForFile(fp, segCount, segSize, fakepool.SegmentBehavior{
		Latency: 1 * time.Millisecond,
	})

	mvf := newTestMVF(t, ctx, fp, segCount, segSize, maxPrefetch)
	fileSize := int64(segCount * segSize)

	// Deterministic RNG so the test is reproducible. Restrict the access
	// pattern to hotWindowSegs distinct segments so the working set fits
	// in the per-file LRU.
	rng := rand.New(rand.NewSource(42))
	buf := make([]byte, readSize)
	for i := 0; i < readCount; i++ {
		segIdx := rng.Intn(hotWindowSegs)
		off := int64(segIdx) * int64(segSize)
		if off+int64(readSize) > fileSize {
			off = fileSize - int64(readSize)
		}
		if _, err := mvf.ReadAt(buf, off); err != nil {
			t.Fatalf("ReadAt #%d at %d: %v", i, off, err)
		}
	}

	calls := fp.BodyPriorityCalls()
	t.Logf("%d ReadAt calls within %d-segment hot window produced %d BodyPriority requests "+
		"(invariant: calls <= 2 × hotWindowSegs)", readCount, hotWindowSegs, calls)

	// PINNED INVARIANT: with the LRU cache, only the unique hot-window
	// segments are fetched (plus a small slop for first-call shared-path
	// overlap and any cache miss before warm-up).
	const budget = 2 * hotWindowSegs
	if calls > int64(budget) {
		t.Errorf("INVARIANT regression (S5): BodyPriority=%d, want <= %d "+
			"(should be roughly one fetch per unique segment in the hot window). "+
			"The per-file random-read LRU is no longer coalescing repeated reads "+
			"within the same segment.",
			calls, budget)
	}
}

// TestRandomReadCache_EOFReadDoesNotPanic regression-pins a bug where a
// ReadAt that straddles end-of-file panics inside
// tryServeFromRandomReadCache. The ephemeral path clamps `end` to
// FileSize-1 but `len(p)` is left at the original FUSE read size (e.g.
// 16 KiB). The cache then slices `full[rel : rel + len(p)]` past the
// segment's actual size:
//
//   panic: runtime error: slice bounds out of range [:704512] with capacity 703432
//
// reproduced on a Jellyfin library scan against an .mp4 whose last
// segment is partially-filled. The fix clamps the copy length to the
// clamped read window (end-off+1), capped by len(p).
//
// Test setup: a file whose last segment is the only one with usable
// data < SegmentSize, so the EOF straddle only ever affects the final
// segment. ReadAt is issued at a position close enough to FileSize that
// off + len(p) > FileSize but off + len(p) <= segStart + segSize.
func TestRandomReadCache_EOFReadDoesNotPanic(t *testing.T) {
	t.Parallel()
	const (
		fullSegs   = 3
		segSize    = 1024
		tailUsable = 700 // last segment has only 700 readable bytes
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	fp := fakepool.New()
	// Full segments: every byte is readable.
	for i := 0; i < fullSegs; i++ {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{
			Bytes: segments.Payload(i, segSize),
		})
	}
	// Tail segment: only the first tailUsable bytes are valid file data,
	// the rest is "padding" inside the segment's wire payload. The fake
	// returns segSize bytes either way; FileSize is what stops the file
	// short.
	fp.SetBehavior(segments.MessageID(fullSegs), fakepool.SegmentBehavior{
		Bytes: segments.Payload(fullSegs, segSize),
	})

	segData := make([]*metapb.SegmentData, fullSegs+1)
	for i := range segData {
		segData[i] = &metapb.SegmentData{
			Id:          segments.MessageID(i),
			SegmentSize: int64(segSize),
			StartOffset: 0,
			EndOffset:   int64(segSize - 1),
		}
	}
	fileSize := int64(fullSegs*segSize + tailUsable)

	mvf := &MetadataVirtualFile{
		name: "test-eof-readat",
		meta: &fileHandleMeta{
			FileSize:    fileSize,
			SegmentData: segData,
		},
		poolManager:      newFakePoolManager(fp),
		ctx:              ctx,
		maxPrefetch:      4,
		originalRangeEnd: -1,
		streamTracker:    noopStreamTracker{},
		streamID:         "eof-stream",
	}
	t.Cleanup(func() { _ = mvf.Close() })

	// Issue a 4 KiB ReadAt at an offset where off + len(p) > FileSize but
	// off + len(p) <= segStart + segSize. This is the exact straddle that
	// panicked in production.
	buf := make([]byte, 4096)
	off := int64(fullSegs * segSize) // start of the partial tail segment
	n, err := mvf.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		t.Fatalf("ReadAt across EOF returned unexpected error: %v", err)
	}
	if int64(n) != tailUsable {
		t.Errorf("ReadAt at EOF returned n=%d, want %d (the readable tail)", n, tailUsable)
	}

	// Issue the same read again so the second call lands on the cache-hit
	// arm (the first call populates the LRU on success). Same straddle
	// invariant must hold there.
	n2, err := mvf.ReadAt(buf, off)
	if err != nil && err != io.EOF {
		t.Fatalf("cache-hit ReadAt across EOF returned unexpected error: %v", err)
	}
	if int64(n2) != tailUsable {
		t.Errorf("cache-hit ReadAt at EOF returned n=%d, want %d", n2, tailUsable)
	}
}

// TestStorm_SeekSpamAccumulatesCloserGoroutines pins the post-S6
// invariant: rapid Seek calls between unaligned positions are absorbed
// by a bounded closer-worker pool (closerWorkerCount=4 workers per
// file), so peak goroutine growth from seek-spam stays at a small
// constant regardless of seek rate. Without the bound, every Seek
// spawned its own closer goroutine via closeWg.Go (unbounded).
//
// We bypass the real UsenetReader and install a slowCloseReader directly
// into mvf.reader between each Seek. slowCloseReader.Close sleeps for
// closeDelay, modeling a real UsenetReader.Close that is waiting for
// in-flight BodyPriority calls to drain. With 50 seeks each installing a
// reader whose Close takes 1s, the unbounded closeWg.Go fan-out produces
// ~50 concurrently-pinned goroutines.
//
// Driving Seek through the real Read path (which would download segments)
// would be slow and racy; the install-directly approach exercises exactly
// the storm-relevant code path (closeCurrentReader → closeWg.Go) while
// giving us deterministic timing.
//
// CURRENT BEHAVIOR: NumGoroutine grows by ~one goroutine per seek (one
// closer waiting inside slowCloseReader.Close).
//
// TARGET INVARIANT (after fix): a bounded closer pool caps the number of
// pending closes at a small constant regardless of seek rate.
func TestStorm_SeekSpamAccumulatesCloserGoroutines(t *testing.T) {
	t.Parallel()
	const (
		segCount    = 100
		segSize     = 1024
		seekCount   = 50
		maxPrefetch = 2
		// Close takes ~1s so all closer goroutines stay pinned for the
		// duration of the seek burst.
		closeDelay = 1 * time.Second
	)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fp := fakepool.New()
	configurePoolForFile(fp, segCount, segSize, fakepool.SegmentBehavior{
		Latency: 1 * time.Millisecond,
	})

	mvf := newTestMVF(t, ctx, fp, segCount, segSize, maxPrefetch)
	fileSize := int64(segCount * segSize)

	// Track total slow-close readers created so the test can sanity-check
	// they were all properly handed to closeWg.
	var totalReaders int32

	// Install the first reader so the first Seek has something to close.
	primeReader := newSlowCloseReader(closeDelay)
	totalReaders++
	mvf.mu.Lock()
	mvf.reader = primeReader
	mvf.readerInitialized = true
	mvf.position = 0
	mvf.mu.Unlock()

	snap := goroutines.Take(t)

	for i := 0; i < seekCount; i++ {
		// Seek to a different position each time so closeCurrentReader fires.
		var off int64
		if i%2 == 0 {
			off = int64(i+1) * int64(segSize)
		} else {
			off = fileSize - int64(i+1)*int64(segSize)
		}
		if _, err := mvf.Seek(off, io.SeekStart); err != nil {
			t.Fatalf("Seek #%d: %v", i, err)
		}
		// closeCurrentReader has nil-ed mvf.reader; install a fresh
		// slow-close reader so the NEXT Seek has something to close.
		next := newSlowCloseReader(closeDelay)
		totalReaders++
		mvf.mu.Lock()
		mvf.reader = next
		mvf.readerInitialized = true
		mvf.mu.Unlock()
	}

	// Sample peak goroutine count immediately after the seek burst,
	// while the closer workers are still draining queued readers.
	peak := runtime.NumGoroutine()
	delta := peak - snap.Baseline()
	t.Logf("%d seeks installed %d readers; peak goroutines=%d (delta=%d from baseline %d) "+
		"(invariant: delta <= 2 × closer-pool size)",
		seekCount, totalReaders, peak, delta, snap.Baseline())

	// PINNED INVARIANT: goroutine delta stays bounded by the closer
	// pool size (currently 4) regardless of seek count. Generous 2x
	// budget to absorb the runtime's helper goroutines that may also
	// be scheduled during the burst.
	const budget = 2 * 4 // 2 × closerWorkerCount
	if delta > budget {
		t.Errorf("INVARIANT regression (S6): goroutine delta=%d, want <= %d after %d seeks. "+
			"closeCurrentReader is no longer routing into the bounded closer-worker pool.",
			delta, budget, seekCount)
	}
}
