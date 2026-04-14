package backend

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

// mockSource implements readAtContexter for testing.
type mockSource struct {
	data    []byte
	mu      sync.Mutex
	delay   time.Duration // per-read delay to simulate slow source
	readErr error         // error to return after all data consumed
}

func (m *mockSource) ReadAtContext(_ context.Context, p []byte, off int64) (int, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if off >= int64(len(m.data)) {
		if m.readErr != nil {
			return 0, m.readErr
		}
		return 0, io.EOF
	}
	n := copy(p, m.data[off:])
	return n, nil
}

func TestAsyncReadBuffer_BasicSequentialRead(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefghijklmnop"), 1024) // 16KB
	src := &mockSource{data: data}
	buf := NewAsyncReadBuffer(context.Background(), src, 4096, int64(len(data)))
	defer buf.Close()

	got := make([]byte, 0, len(data))
	p := make([]byte, 256)
	off := int64(0)
	for off < int64(len(data)) {
		n, err := buf.ReadAtContext(context.Background(), p, off)
		if n > 0 {
			got = append(got, p[:n]...)
			off += int64(n)
		}
		if err != nil {
			if err == io.EOF && off >= int64(len(data)) {
				break
			}
			t.Fatalf("unexpected error at offset %d: %v", off, err)
		}
	}
	if !bytes.Equal(got, data) {
		t.Fatalf("data mismatch: got %d bytes, want %d bytes", len(got), len(data))
	}
}

func TestAsyncReadBuffer_SlowSource(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 8192)
	src := &mockSource{data: data, delay: 5 * time.Millisecond}
	// Large buffer relative to data — should fill ahead.
	buf := NewAsyncReadBuffer(context.Background(), src, 16384, int64(len(data)))
	defer buf.Close()

	// Wait briefly for buffer to fill.
	time.Sleep(200 * time.Millisecond)

	// Reads should now be instant.
	p := make([]byte, 4096)
	start := time.Now()
	n, err := buf.ReadAtContext(context.Background(), p, 0)
	dur := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 4096 {
		t.Fatalf("got %d bytes, want 4096", n)
	}
	if dur > 10*time.Millisecond {
		t.Fatalf("read took %v, expected instant (data should be buffered)", dur)
	}
}

func TestAsyncReadBuffer_ErrorPropagation(t *testing.T) {
	data := []byte("hello")
	src := &mockSource{data: data}
	buf := NewAsyncReadBuffer(context.Background(), src, 1024, int64(len(data)))
	defer buf.Close()

	// Read all data.
	p := make([]byte, 10)
	n, _ := buf.ReadAtContext(context.Background(), p, 0)
	if n != 5 {
		t.Fatalf("got %d bytes, want 5", n)
	}

	// Next read should return EOF.
	n, err := buf.ReadAtContext(context.Background(), p, 5)
	if err != io.EOF {
		t.Fatalf("expected io.EOF, got n=%d err=%v", n, err)
	}
}

func TestAsyncReadBuffer_CloseDuringFill(t *testing.T) {
	// Very slow source — fill goroutine will be blocked.
	data := bytes.Repeat([]byte("x"), 1024*1024)
	src := &mockSource{data: data, delay: 100 * time.Millisecond}
	buf := NewAsyncReadBuffer(context.Background(), src, 4096, int64(len(data)))

	// Close immediately — should not hang.
	done := make(chan struct{})
	go func() {
		buf.Close()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Close() hung")
	}
}

func TestAsyncReadBuffer_OutOfRangePassthrough(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 1024) // 8KB
	src := &mockSource{data: data}
	buf := NewAsyncReadBuffer(context.Background(), src, 2048, int64(len(data)))
	defer buf.Close()

	// Read the first 1024 bytes to advance the buffer.
	p := make([]byte, 1024)
	n, err := buf.ReadAtContext(context.Background(), p, 0)
	if err != nil || n != 1024 {
		t.Fatalf("first read: n=%d err=%v", n, err)
	}

	// Read at offset 6000 — far outside the buffer window.
	// Should pass through to source directly.
	p2 := make([]byte, 100)
	n, err = buf.ReadAtContext(context.Background(), p2, 6000)
	if err != nil {
		t.Fatalf("passthrough read: err=%v", err)
	}
	if n != 100 {
		t.Fatalf("passthrough read: n=%d, want 100", n)
	}
	if !bytes.Equal(p2[:n], data[6000:6100]) {
		t.Fatal("passthrough read: data mismatch")
	}
}

func TestAsyncReadBuffer_GetBufferedOffset(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 4096)
	src := &mockSource{data: data}
	buf := NewAsyncReadBuffer(context.Background(), src, 8192, int64(len(data)))
	defer buf.Close()

	// Trigger fill by doing a small read (fill is lazy).
	p := make([]byte, 1)
	buf.ReadAtContext(context.Background(), p, 0)

	// Wait for fill to complete.
	time.Sleep(100 * time.Millisecond)

	off := buf.GetBufferedOffset()
	// After reading 1 byte, baseOff=1, fill should have read all 4096 bytes.
	// So buffered offset = 4096 (baseOff=1 + filled=4095).
	if off != int64(len(data)) {
		t.Fatalf("GetBufferedOffset() = %d, want %d", off, len(data))
	}

	// Consume some data.
	p = make([]byte, 1024)
	buf.ReadAtContext(context.Background(), p, 1)
	off = buf.GetBufferedOffset()
	// baseOff advanced by 1024, filled reduced by 1024, but fillOff didn't change.
	if off != int64(len(data)) {
		t.Fatalf("after read: GetBufferedOffset() = %d, want %d", off, len(data))
	}
}

func TestAsyncReadBuffer_ZeroLengthRead(t *testing.T) {
	src := &mockSource{data: []byte("hello")}
	buf := NewAsyncReadBuffer(context.Background(), src, 1024, 5)
	defer buf.Close()

	n, err := buf.ReadAtContext(context.Background(), nil, 0)
	if n != 0 || err != nil {
		t.Fatalf("zero-length read: n=%d err=%v", n, err)
	}
}

func TestAsyncReadBuffer_SeekThenSequential(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 2048) // 16KB
	src := &mockSource{data: data}
	buf := NewAsyncReadBuffer(context.Background(), src, 4096, int64(len(data)))
	defer buf.Close()

	// Read first 1KB sequentially.
	p := make([]byte, 1024)
	n, err := buf.ReadAtContext(context.Background(), p, 0)
	if err != nil || n != 1024 {
		t.Fatalf("first read: n=%d err=%v", n, err)
	}
	if !bytes.Equal(p[:n], data[:1024]) {
		t.Fatal("first read: data mismatch")
	}

	// Seek to offset 8000 (non-sequential — triggers reset).
	n, err = buf.ReadAtContext(context.Background(), p, 8000)
	if err != nil || n != 1024 {
		t.Fatalf("seek read: n=%d err=%v", n, err)
	}
	if !bytes.Equal(p[:n], data[8000:9024]) {
		t.Fatal("seek read: data mismatch")
	}

	// Continue reading sequentially from 9024 (should use buffer, not passthrough).
	n, err = buf.ReadAtContext(context.Background(), p, 9024)
	if err != nil || n != 1024 {
		t.Fatalf("post-seek sequential read: n=%d err=%v", n, err)
	}
	if !bytes.Equal(p[:n], data[9024:10048]) {
		t.Fatal("post-seek sequential read: data mismatch")
	}
}

func TestAsyncReadBuffer_ConcurrentReadClose(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1024*1024)
	src := &mockSource{data: data, delay: time.Millisecond}
	buf := NewAsyncReadBuffer(context.Background(), src, 4096, int64(len(data)))

	var wg sync.WaitGroup
	wg.Add(2)

	// Reader goroutine.
	go func() {
		defer wg.Done()
		p := make([]byte, 512)
		off := int64(0)
		for i := 0; i < 100; i++ {
			n, err := buf.ReadAtContext(context.Background(), p, off)
			if err != nil {
				return
			}
			off += int64(n)
		}
	}()

	// Close after a short delay.
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		buf.Close()
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent read+close hung")
	}
}
