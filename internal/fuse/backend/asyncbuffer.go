package backend

import (
	"context"
	"io"
	"sync"
)

const (
	// defaultAsyncBufSize is the default read-ahead buffer size (8MB).
	defaultAsyncBufSize = 8 * 1024 * 1024
	// fillChunkSize is how much the background goroutine reads per iteration.
	// Larger chunks reduce mutex acquisition overhead in ReadAtContext.
	// Segments are ~750KB, so 1MB reads ~1 ReadAtContext call per segment.
	fillChunkSize = 1024 * 1024 // 1MB
)

// readAtContexter matches nzbfilesystem.MetadataVirtualFile.ReadAtContext.
type readAtContexter interface {
	ReadAtContext(ctx context.Context, p []byte, off int64) (n int, err error)
}

// AsyncReadBuffer wraps a readAtContexter and continuously reads ahead
// into a ring buffer. FUSE reads pull from the pre-filled buffer instead
// of blocking on the underlying (network-backed) reader.
type AsyncReadBuffer struct {
	src      readAtContexter
	ctx      context.Context
	cancel   context.CancelFunc
	fileSize int64

	mu   sync.Mutex
	cond *sync.Cond

	buf     []byte
	bufSize int
	readPos int // read cursor in ring buffer
	filled  int // bytes currently in buffer

	baseOff int64 // absolute file offset corresponding to readPos
	fillOff int64 // absolute file offset of next fill read

	srcErr  error // terminal error from source
	srcDone bool  // fill goroutine finished
	started bool  // fill goroutine has been launched

	closeOnce sync.Once
	wg        sync.WaitGroup
}

// NewAsyncReadBuffer creates an async read-ahead buffer wrapping src.
// The fill goroutine is started lazily on the first ReadAtContext call
// to avoid allocating memory for files that are opened but never read.
func NewAsyncReadBuffer(ctx context.Context, src readAtContexter, bufSize int, fileSize int64) *AsyncReadBuffer {
	if bufSize <= 0 {
		bufSize = defaultAsyncBufSize
	}
	ctx, cancel := context.WithCancel(ctx)
	a := &AsyncReadBuffer{
		src:      src,
		ctx:      ctx,
		cancel:   cancel,
		fileSize: fileSize,
		bufSize:  bufSize,
	}
	a.cond = sync.NewCond(&a.mu)
	return a
}

// startFill launches the background fill goroutine. Must be called with a.mu held.
func (a *AsyncReadBuffer) startFill() {
	if a.started {
		return
	}
	a.started = true
	a.buf = make([]byte, a.bufSize)
	a.wg.Add(1)
	go a.fill()
}

// fill continuously reads from the source into the ring buffer.
func (a *AsyncReadBuffer) fill() {
	defer a.wg.Done()
	tmp := make([]byte, fillChunkSize)

	for {
		if a.ctx.Err() != nil {
			a.mu.Lock()
			a.srcErr = a.ctx.Err()
			a.srcDone = true
			a.cond.Broadcast()
			a.mu.Unlock()
			return
		}

		// Wait if buffer is full.
		a.mu.Lock()
		for a.filled >= a.bufSize && a.ctx.Err() == nil {
			a.cond.Wait()
		}
		if a.ctx.Err() != nil {
			a.srcErr = a.ctx.Err()
			a.srcDone = true
			a.cond.Broadcast()
			a.mu.Unlock()
			return
		}
		space := a.bufSize - a.filled
		fillOff := a.fillOff
		a.mu.Unlock()

		// Check if we've reached the end of the file.
		if a.fileSize > 0 && fillOff >= a.fileSize {
			a.mu.Lock()
			a.srcErr = io.EOF
			a.srcDone = true
			a.cond.Broadcast()
			a.mu.Unlock()
			return
		}

		// Read from source outside the lock — this is the potentially blocking call.
		toRead := min(len(tmp), space)
		if a.fileSize > 0 && fillOff+int64(toRead) > a.fileSize {
			toRead = int(a.fileSize - fillOff)
		}
		n, err := a.src.ReadAtContext(a.ctx, tmp[:toRead], fillOff)

		// Copy into ring buffer.
		a.mu.Lock()
		if n > 0 {
			writePos := (a.readPos + a.filled) % a.bufSize
			// Handle wrap-around: may need two copies.
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

		if err != nil {
			return
		}
	}
}

// ReadAtContext reads from the async buffer at the given offset.
// Sequential reads are served from the buffer. Non-sequential reads
// that fall outside the buffer window pass through to the source directly.
func (a *AsyncReadBuffer) ReadAtContext(ctx context.Context, p []byte, off int64) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	a.mu.Lock()
	a.startFill()

	// Check if the offset is within our buffer window.
	bufEnd := a.baseOff + int64(a.filled)
	if off >= a.baseOff && off < bufEnd {
		// Data is already in the buffer — serve it.
		n := a.copyFromBuffer(p, off)
		a.mu.Unlock()
		return n, nil
	}

	// If sequential (offset matches buffer frontier), wait for data.
	if off == bufEnd && !a.srcDone {
		for a.baseOff+int64(a.filled) <= off && !a.srcDone && ctx.Err() == nil {
			a.cond.Wait()
		}
		if ctx.Err() != nil {
			a.mu.Unlock()
			return 0, ctx.Err()
		}
		if a.filled > 0 && off >= a.baseOff && off < a.baseOff+int64(a.filled) {
			n := a.copyFromBuffer(p, off)
			a.mu.Unlock()
			return n, nil
		}
		// Buffer is empty and source is done.
		if a.srcDone {
			err := a.srcErr
			a.mu.Unlock()
			return 0, err
		}
	}

	// Source done and offset matches frontier — return the error.
	if off == bufEnd && a.srcDone {
		err := a.srcErr
		a.mu.Unlock()
		return 0, err
	}

	a.mu.Unlock()

	// Non-sequential or out-of-range: pass through directly to source.
	return a.src.ReadAtContext(ctx, p, off)
}

// copyFromBuffer copies data from the ring buffer into p starting at file offset off.
// Caller must hold a.mu. Advances readPos and baseOff, draining consumed data.
func (a *AsyncReadBuffer) copyFromBuffer(p []byte, off int64) int {
	// Skip any bytes between baseOff and off (data we don't need).
	skip := int(off - a.baseOff)
	if skip > 0 {
		a.readPos = (a.readPos + skip) % a.bufSize
		a.filled -= skip
		a.baseOff += int64(skip)
	}

	n := min(len(p), a.filled)
	// Handle wrap-around.
	first := min(n, a.bufSize-a.readPos)
	copy(p[:first], a.buf[a.readPos:a.readPos+first])
	if first < n {
		copy(p[first:n], a.buf[:n-first])
	}
	a.readPos = (a.readPos + n) % a.bufSize
	a.filled -= n
	a.baseOff += int64(n)
	a.cond.Signal() // wake fill goroutine — room available
	return n
}

// GetBufferedOffset returns the file offset up to which data is buffered.
func (a *AsyncReadBuffer) GetBufferedOffset() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.baseOff + int64(a.filled)
}

// Close stops the fill goroutine and releases resources.
// It does NOT close the underlying source — the FUSE handle owns that lifecycle.
func (a *AsyncReadBuffer) Close() {
	a.closeOnce.Do(func() {
		a.cancel()
		a.mu.Lock()
		a.srcDone = true
		a.cond.Broadcast()
		a.mu.Unlock()
		a.wg.Wait()
	})
}
