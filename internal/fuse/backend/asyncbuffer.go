package backend

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// defaultAsyncBufSize is the default read-ahead buffer size (8MB).
	defaultAsyncBufSize = 8 * 1024 * 1024
	// fillChunkSize is how much the background goroutine reads per iteration.
	// Larger chunks reduce mutex churn in ReadAtContext. Segments are ~750KB,
	// so 4MB reads ~1 ReadAtContext call per segment.
	fillChunkSize = 4 * 1024 * 1024 // 4MB
	// nearFrontierWindow is the lookahead beyond the fill frontier treated as
	// near-sequential during streaming. Set equal to fillChunkSize so that a
	// near-frontier read is guaranteed to be satisfied within at most one fill
	// iteration.
	nearFrontierWindow = 4 * 1024 * 1024 // 4MB — kept equal to fillChunkSize intentionally
	// probingSeqTolerance is the maximum forward delta in probing mode that is
	// still counted as sequential for arming read-ahead. Kernel readahead may
	// issue 128 KB parallel probes slightly out of order; 256 KB absorbs that
	// without creating large holes in the streaming start frontier.
	probingSeqTolerance = 256 * 1024 // 256 KB
	// armThreshold is how many consecutive sequential reads must be observed
	// before the buffer promotes from probing to streaming and begins reading
	// ahead. This keeps read-ahead off during the media player's header-probe
	// phase and during seek/scrub bursts (which never produce a sustained
	// sequential run), addressing the seek-thrashing that got earlier versions
	// of this buffer reverted.
	armThreshold = 3
	// closeDrainTimeout bounds how long Close waits for the fill goroutine to
	// exit. ctx cancellation propagates into the source read, so this is a
	// safety net rather than the normal path.
	closeDrainTimeout = 5 * time.Second
)

// readAtContexter matches nzbfilesystem.MetadataVirtualFile.ReadAtContext.
type readAtContexter interface {
	ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error)
}

// Global read-ahead memory budget shared across all open buffers. A value of
// 0 means unlimited (no accounting). Buffers reserve their size on promotion
// and release it on demotion/close, so memory is only held by handles that are
// actively streaming.
var (
	globalBudgetMax  atomic.Int64 // bytes; 0 = unlimited
	globalBudgetUsed atomic.Int64 // bytes currently reserved
)

// SetAsyncBufferBudget sets the global cap (in bytes) on total read-ahead
// memory across all open buffers. 0 disables accounting (unlimited).
func SetAsyncBufferBudget(maxBytes int64) {
	if maxBytes < 0 {
		maxBytes = 0
	}
	globalBudgetMax.Store(maxBytes)
}

// reserveBudget attempts to reserve n bytes from the global budget. Returns
// (granted, accounted): accounted is true only when the reservation was
// recorded in globalBudgetUsed (i.e. a finite budget is configured), so the
// caller knows whether a matching release is required.
func reserveBudget(n int64) (granted bool, accounted bool) {
	max := globalBudgetMax.Load()
	if max <= 0 {
		return true, false // unlimited — no accounting
	}
	for {
		used := globalBudgetUsed.Load()
		if used+n > max {
			return false, false
		}
		if globalBudgetUsed.CompareAndSwap(used, used+n) {
			return true, true
		}
	}
}

func releaseBudget(n int64) {
	globalBudgetUsed.Add(-n)
}

// AsyncReadBuffer wraps a readAtContexter and reads ahead into a ring buffer so
// FUSE reads pull from pre-filled memory instead of blocking on the
// network-backed source. This mirrors the client-side read-ahead that rclone's
// VFS provides over the WebDAV endpoint, which the native FUSE mount otherwise
// lacks.
//
// State machine (the key difference from the earlier reverted design, which
// reset and refilled on every non-sequential read and thus thrashed under
// Plex/Jellyfin seek bursts):
//
//   - probing:   no buffer allocated, no fill goroutine work; every read is a
//     direct passthrough to the source. Consecutive sequential reads are
//     counted.
//   - streaming: after armThreshold sustained sequential reads, the buffer is
//     allocated (subject to the global memory budget) and a single fill
//     goroutine reads ahead. A non-sequential read (seek) demotes back to
//     probing, freeing the buffer and budget; read-ahead only re-arms after
//     sequential reads resume.
//
// A single fill goroutine is started lazily on first promotion and parked on
// the cond while probing, so promote/demote cycles never spawn goroutines.
type AsyncReadBuffer struct {
	src      readAtContexter
	ctx      context.Context
	cancel   context.CancelFunc
	fileSize int64
	bufSize  int
	log      *slog.Logger

	mu   sync.Mutex
	cond *sync.Cond

	// Ring buffer — allocated on promotion, freed on demotion/close.
	buf     []byte
	readPos int   // read cursor in ring buffer
	filled  int   // bytes currently buffered
	baseOff int64 // absolute file offset corresponding to readPos
	fillOff int64 // absolute file offset of the next fill read

	gen     uint64 // bumped on promote/demote to invalidate in-flight fills
	srcErr  error  // terminal error from source (within current generation)
	srcDone bool   // source reached EOF/err for current generation

	// State machine.
	streaming    bool  // true = buffered + filling; false = probing/passthrough
	expectedNext int64 // next sequential offset expected
	seqRun       int   // consecutive sequential reads observed while probing
	accounted    bool  // holds an accounted slot in the global budget

	started   bool // fill goroutine launched
	closed    bool
	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewAsyncReadBuffer creates an async read-ahead buffer wrapping src. The fill
// goroutine is started lazily on first promotion, so opening a file that is
// only probed (header reads) and closed never allocates the buffer or spawns a
// goroutine.
func NewAsyncReadBuffer(ctx context.Context, src readAtContexter, bufSize int, fileSize int64, log *slog.Logger) *AsyncReadBuffer {
	if bufSize <= 0 {
		bufSize = defaultAsyncBufSize
	}
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithCancel(ctx)
	a := &AsyncReadBuffer{
		src:      src,
		ctx:      ctx,
		cancel:   cancel,
		fileSize: fileSize,
		bufSize:  bufSize,
		log:      log,
	}
	a.cond = sync.NewCond(&a.mu)
	return a
}

// ReadAtContext serves a read at the given absolute offset. Sequential reads in
// streaming mode are served from the ring buffer; everything else is a direct
// passthrough to the source while the sequential-run counter decides whether to
// (re)arm read-ahead.
func (a *AsyncReadBuffer) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	a.mu.Lock()
	a.markSourceDoneIfCanceledLocked()
	if a.closed {
		a.mu.Unlock()
		return a.src.ReadAtContext(ctx, p, off)
	}

	if a.streaming {
		// bufEnd is a snapshot of the fill frontier taken once; the inner-loop
		// conditions use live a.baseOff+int64(a.filled) so they see progress
		// made by the fill goroutine while we wait.
		bufEnd := a.baseOff + int64(a.filled)

		// Offset already within the buffered window.
		if off >= a.baseOff && off < bufEnd {
			n := a.copyFromBuffer(p, off)
			a.expectedNext = off + int64(n)
			a.mu.Unlock()
			return n, nil
		}

		// Sequential read at the buffer frontier — wait for the fill goroutine.
		if off == bufEnd && off == a.expectedNext {
			for a.baseOff+int64(a.filled) <= off && !a.srcDone && !a.closed && ctx.Err() == nil {
				a.cond.Wait()
			}
			if ctx.Err() != nil {
				a.mu.Unlock()
				return 0, ctx.Err()
			}
			if !a.closed && off >= a.baseOff && off < a.baseOff+int64(a.filled) {
				n := a.copyFromBuffer(p, off)
				a.expectedNext = off + int64(n)
				a.mu.Unlock()
				return n, nil
			}
			if a.srcDone {
				err := a.srcErr
				a.mu.Unlock()
				return 0, err
			}
			// Closed while waiting — fall through to passthrough.
		}

		// Near-frontier: this read is just ahead of the fill frontier — likely a
		// kernel readahead parallel read, not a genuine seek. Wait for the fill
		// goroutine to reach this offset rather than demoting.
		if off >= bufEnd && off < bufEnd+nearFrontierWindow {
			for !a.srcDone && !a.closed && ctx.Err() == nil {
				if a.baseOff+int64(a.filled) > off {
					break
				}
				// If the buffer is full the fill goroutine is blocked waiting for
				// consumers to drain it — waiting here would deadlock, so give up
				// and fall through to demote instead.
				if a.filled >= a.bufSize {
					// Buffer full — fill goroutine cannot advance; demote rather than deadlock.
					goto demote
				}
				a.cond.Wait()
			}
			if ctx.Err() != nil {
				a.mu.Unlock()
				return 0, ctx.Err()
			}
			if !a.closed && off >= a.baseOff && off < a.baseOff+int64(a.filled) {
				n := a.copyFromBuffer(p, off)
				a.expectedNext = off + int64(n)
				a.mu.Unlock()
				return n, nil
			}
			if a.srcDone {
				err := a.srcErr
				a.mu.Unlock()
				return 0, err
			}
			// Closed while waiting or off fell outside buffer — fall through to demote.
		}
		// Non-sequential read (seek) while streaming → demote to probing.
	demote:
		a.demoteLocked()
	}

	// Probing mode: count sequential runs, decide on promotion, then serve the
	// read directly from the source (outside the lock).
	// Treat reads within probingSeqTolerance ahead as sequential for arming
	// purposes — kernel readahead may issue 128 KB parallel probes slightly out
	// of order; 256 KB absorbs that without creating large holes in the
	// streaming start frontier.
	if off >= a.expectedNext && off <= a.expectedNext+probingSeqTolerance {
		a.seqRun++
	} else {
		a.seqRun = 1
	}
	shouldPromote := a.seqRun >= armThreshold && a.fileSize > int64(a.bufSize)
	a.mu.Unlock()

	n, err := a.src.ReadAtContext(ctx, p, off)

	a.mu.Lock()
	a.expectedNext = off + int64(n)
	if shouldPromote && n > 0 && !a.closed && !a.streaming {
		a.promoteLocked(off + int64(n))
	}
	a.mu.Unlock()
	return n, err
}

// promoteLocked allocates the ring buffer and (re)starts read-ahead from
// frontier. No-op if the global memory budget is exhausted (stays probing).
// Caller must hold a.mu.
func (a *AsyncReadBuffer) promoteLocked(frontier int64) {
	if a.streaming {
		return
	}
	granted, accounted := reserveBudget(int64(a.bufSize))
	if !granted {
		// Over budget — keep serving directly. seqRun stays high, so the next
		// sequential read retries the (cheap) reservation.
		return
	}
	a.accounted = accounted
	if a.buf == nil {
		a.buf = make([]byte, a.bufSize)
	}
	a.baseOff = frontier
	a.fillOff = frontier
	a.readPos = 0
	a.filled = 0
	a.srcErr = nil
	a.srcDone = false
	a.gen++
	a.streaming = true

	if !a.started {
		a.started = true
		a.wg.Add(1)
		go a.fill()
	} else {
		a.cond.Broadcast() // wake the parked fill goroutine
	}
}

// demoteLocked tears down read-ahead and returns to probing mode, freeing the
// buffer and releasing the budget so memory is only held while streaming.
// Caller must hold a.mu.
func (a *AsyncReadBuffer) demoteLocked() {
	if !a.streaming {
		return
	}
	a.streaming = false
	a.filled = 0
	a.readPos = 0
	a.buf = nil
	a.srcDone = false
	a.srcErr = nil
	a.gen++ // invalidate any in-flight fill read
	if a.accounted {
		releaseBudget(int64(a.bufSize))
		a.accounted = false
	}
	a.cond.Broadcast() // wake the fill goroutine so it re-parks
}

// fill is the single background goroutine. It parks on the cond while probing
// and reads ahead while streaming. Started lazily on first promotion.
func (a *AsyncReadBuffer) fill() {
	defer a.wg.Done()
	tmp := make([]byte, fillChunkSize)

	for {
		a.mu.Lock()
		// Park while not actively streaming.
		for !a.streaming && !a.closed && a.ctx.Err() == nil {
			a.cond.Wait()
		}
		if a.closed || a.ctx.Err() != nil {
			a.markSourceDoneIfCanceledLocked()
			a.mu.Unlock()
			return
		}
		// Park while the buffer is full or the current generation is done.
		for a.streaming && (a.filled >= a.bufSize || a.srcDone) && !a.closed && a.ctx.Err() == nil {
			a.cond.Wait()
		}
		if a.closed || a.ctx.Err() != nil {
			a.markSourceDoneIfCanceledLocked()
			a.mu.Unlock()
			return
		}
		if !a.streaming {
			a.mu.Unlock()
			continue // demoted while waiting — re-park
		}

		// End of file for this generation.
		if a.fileSize > 0 && a.fillOff >= a.fileSize {
			a.srcErr = io.EOF
			a.srcDone = true
			a.cond.Broadcast()
			a.mu.Unlock()
			continue
		}

		space := a.bufSize - a.filled
		fillOff := a.fillOff
		myGen := a.gen
		toRead := min(fillChunkSize, space)
		if a.fileSize > 0 && fillOff+int64(toRead) > a.fileSize {
			toRead = int(a.fileSize - fillOff)
		}
		a.mu.Unlock()

		// Blocking source read happens outside the lock.
		n, err := a.src.ReadAtContext(a.ctx, tmp[:toRead], fillOff)

		a.mu.Lock()
		if a.gen != myGen || !a.streaming || a.closed {
			// Demoted / seek / closed while reading — discard.
			a.mu.Unlock()
			continue
		}
		if n > 0 {
			writePos := (a.readPos + a.filled) % a.bufSize
			first := min(n, a.bufSize-writePos)
			copy(a.buf[writePos:writePos+first], tmp[:first])
			if first < n {
				copy(a.buf[:n-first], tmp[first:n])
			}
			a.filled += n
			a.fillOff += int64(n)
		}
		if err != nil {
			a.srcErr = err
			a.srcDone = true
		}
		a.cond.Broadcast()
		a.mu.Unlock()
	}
}

func (a *AsyncReadBuffer) markSourceDoneIfCanceledLocked() {
	if err := a.ctx.Err(); err != nil && a.streaming && !a.srcDone {
		a.srcErr = err
		a.srcDone = true
		a.cond.Broadcast()
	}
}

// copyFromBuffer copies buffered data into p starting at file offset off,
// draining consumed bytes from the ring. Caller must hold a.mu and must have
// verified off is within [baseOff, baseOff+filled).
func (a *AsyncReadBuffer) copyFromBuffer(p []byte, off int64) int {
	// Skip any bytes between baseOff and off (already-consumed prefix).
	if skip := int(off - a.baseOff); skip > 0 {
		a.readPos = (a.readPos + skip) % a.bufSize
		a.filled -= skip
		a.baseOff += int64(skip)
	}

	n := min(len(p), a.filled)
	first := min(n, a.bufSize-a.readPos)
	copy(p[:first], a.buf[a.readPos:a.readPos+first])
	if first < n {
		copy(p[first:n], a.buf[:n-first])
	}
	a.readPos = (a.readPos + n) % a.bufSize
	a.filled -= n
	a.baseOff += int64(n)
	a.cond.Signal() // room available — wake fill goroutine
	return n
}

// GetBufferedOffset returns the file offset up to which data is currently
// buffered (baseOff+filled), or 0 when probing.
func (a *AsyncReadBuffer) GetBufferedOffset() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.streaming {
		return 0
	}
	return a.baseOff + int64(a.filled)
}

// Close stops the fill goroutine and releases resources. It does NOT close the
// underlying source — the FUSE handle owns that lifecycle. Close drains the
// fill goroutine with a bounded wait: ctx cancellation propagates into the
// source read, so the goroutine normally exits promptly; the timeout is a
// safety net against a wedged source.
func (a *AsyncReadBuffer) Close() {
	a.closeOnce.Do(func() {
		a.cancel()

		a.mu.Lock()
		a.closed = true
		a.srcDone = true
		accounted := a.accounted
		a.accounted = false
		a.cond.Broadcast()
		started := a.started
		a.mu.Unlock()

		if started {
			done := make(chan struct{})
			go func() {
				a.wg.Wait()
				close(done)
			}()

			timer := time.NewTimer(closeDrainTimeout)
			defer timer.Stop()
			ticker := time.NewTicker(200 * time.Millisecond)
			defer ticker.Stop()

		drain:
			for {
				select {
				case <-done:
					break drain
				case <-timer.C:
					a.log.WarnContext(a.ctx, "async read buffer fill goroutine did not exit within timeout")
					break drain
				case <-ticker.C:
					a.mu.Lock()
					a.cond.Broadcast()
					a.mu.Unlock()
				}
			}
		}

		if accounted {
			releaseBudget(int64(a.bufSize))
		}

		a.mu.Lock()
		a.buf = nil
		a.mu.Unlock()
	})
}
