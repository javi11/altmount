# Progressive Segment Delivery Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve a segment's bytes to readers as they arrive off the wire instead of after the whole article is decoded (spec phase 2, defect D1), so first-byte latency drops from one article transfer to roughly one round trip plus the first socket read.

**Architecture:** `segment` becomes a watermark buffer with a per-attempt `io.Writer`; nntppool already streams decoded bytes into a caller writer per socket read and refuses to fail over after partial delivery, so the reader's retry loop starts a second attempt into a fresh buffer and only publishes past the already-served watermark. Streaming readers fetch through a new `BodyStreamPriority` on `pool.NntpClient`; import readers keep the buffered `Body`.

**Tech Stack:** Go, `github.com/javi11/nntppool/v4` (local checkout at `~/mio/nntppool`, developed via `replace`, released as v4.21.0 before the PR is marked ready), `internal/testsupport/fakepool`.

**Spec:** `docs/superpowers/specs/2026-09-04-streaming-demand-shaping-design.md` (Phase 2)

## Global Constraints

- Never mention competitor projects in code, comments, commits, or PR text.
- Conventional Commits; branch `feat/streaming-progressive-delivery` from `feat/streaming-conn-hygiene`.
- `make bench-compare BASE=baseline-main` must show no regression; B1 `ttfb_mean` is expected to drop sharply on both profiles and B2 throughput must hold.
- Existing `internal/usenet` invariant tests stay green under `-race`.
- No `replace` directive may remain in `go.mod` when the PR is marked ready for review.

---

## File structure

| Path | Responsibility |
|---|---|
| `~/mio/nntppool/client.go` | add `BodyStreamPriority` (thin wrapper over `SendPriority` with a writer) |
| `internal/pool/nntpclient.go` | add `BodyStreamPriority` to the `NntpClient` interface |
| `internal/testsupport/fakepool/fakepool.go` | implement `BodyStreamPriority`; add `ChunkSize` and `TailGate` behaviors for progressive tests |
| `internal/usenet/usenet_reader_storm_test.go:146-160` | `multiRecordingClient` delegates the new method |
| `internal/usenet/segment.go` | watermark buffer: `attemptWriter`, `publish`, progressive `segmentReader` |
| `internal/usenet/segment_test.go` | progressive read, second attempt does not rewind, release/cancel unblock |
| `internal/usenet/usenet_reader.go:457-600` | `downloadSegmentWithRetry` streams into the segment for priority readers |
| `internal/usenet/usenet_reader_progressive_test.go` | gate-based tests: bytes served while the tail is held; partial-then-retry yields exact bytes once |
| `go.mod` | temporary `replace` during development; bumped to v4.21.0 at the end |

---

### Task 1: nntppool `BodyStreamPriority`

**Files:**
- Modify: `~/mio/nntppool/client.go` (after `BodyPriority`, line ~96)
- Test: `~/mio/nntppool/client_stream_priority_test.go`

**Interfaces:**
- Produces: `func (c *Client) BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(YEncMeta)) (*ArticleBody, error)`. Decoded bytes go to `w` as they are decoded; `ArticleBody.Bytes` is nil; a CRC mismatch returns both the body and `ErrCRCMismatch`; after partial delivery the client never fails over to another provider and returns the transport error.

- [ ] **Step 1: Write the failing test**

Look at `~/mio/nntppool/stat_coalesce_test.go:43` (`startStatMockServer`) for the local mock-server idiom, then:

```go
package nntppool

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestBodyStreamPriorityWritesDecodedBytesToWriter(t *testing.T) {
	srv := newBodyMockServer(t, 64*1024) // helper from an existing body test; if none exists, reuse the mock from yenc_body_test.go
	defer srv.Close()

	client, err := NewClient(context.Background(), []Provider{{
		Host: srv.Addr(), Connections: 1, Inflight: 1, StatInflight: 1, SkipPing: true, IdleTimeout: time.Hour,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()

	var got bytes.Buffer
	body, err := client.BodyStreamPriority(context.Background(), srv.MessageID(), &got)
	if err != nil {
		t.Fatalf("BodyStreamPriority: %v", err)
	}
	if body.Bytes != nil {
		t.Fatal("streamed bodies must not also buffer Bytes")
	}
	if !bytes.Equal(got.Bytes(), srv.Payload()) {
		t.Fatalf("writer received %d bytes, want %d", got.Len(), len(srv.Payload()))
	}
}
```

Adapt `newBodyMockServer`, `MessageID()` and `Payload()` to whatever the existing body tests in that repo expose (`grep -n "func .*MockServer\|func startBody" ~/mio/nntppool/*_test.go`). Do not add a new mock server if one exists.

- [ ] **Step 2: Run it to verify it fails**

Run: `cd ~/mio/nntppool && go test -run TestBodyStreamPriority ./ -v`
Expected: FAIL, `BodyStreamPriority` undefined.

- [ ] **Step 3: Implement**

```go
// BodyStreamPriority is BodyStream on the priority lane: decoded bytes are
// written to w as each wire read is decoded, so a caller can serve the head
// of an article before its tail has arrived. Bytes is nil on the result. If
// a provider fails after some bytes were written the client does not fail
// over — re-streaming into w would duplicate them — and returns the error;
// callers that want a second attempt must give it a fresh writer.
func (c *Client) BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(YEncMeta)) (*ArticleBody, error) {
	if w == nil {
		return nil, fmt.Errorf("nntp: BodyStreamPriority requires a non-nil writer")
	}
	payload := []byte("BODY <" + messageID + ">\r\n")
	var respCh <-chan Response
	if len(onMeta) > 0 {
		respCh = c.SendPriority(ctx, payload, w, onMeta[0])
	} else {
		respCh = c.SendPriority(ctx, payload, w)
	}
	return c.finishBody(messageID, w, respCh)
}
```

- [ ] **Step 4: Run tests**

Run: `cd ~/mio/nntppool && go test ./ -run 'TestBodyStream|TestBodyPriority' -v && go vet ./`
Expected: PASS.

- [ ] **Step 5: Commit on a branch in nntppool**

```bash
cd ~/mio/nntppool && git checkout -b feat/body-stream-priority && git add client.go client_stream_priority_test.go && git commit -m "feat(client): BodyStreamPriority streams decoded bytes on the priority lane"
```

- [ ] **Step 6: Point altmount at the local checkout**

```bash
cd /Users/javi/orca/workspaces/altmount/halibut && git checkout -b feat/streaming-progressive-delivery feat/streaming-conn-hygiene
go mod edit -replace github.com/javi11/nntppool/v4=/Users/javi/mio/nntppool && go mod tidy && go build ./...
```

Do not commit `go.mod` yet; Task 7 replaces the `replace` with the released version.

---

### Task 2: `NntpClient` interface, fakepool, and test doubles

**Files:**
- Modify: `internal/pool/nntpclient.go:32-35`
- Modify: `internal/testsupport/fakepool/fakepool.go` (`SegmentBehavior`, `serveBody`, new method)
- Modify: `internal/usenet/usenet_reader_storm_test.go:146-160`
- Test: `internal/testsupport/fakepool/fakepool_stream_test.go`

**Interfaces:**
- Produces on `pool.NntpClient`:
  ```go
  // BodyStreamPriority fetches an article on the priority lane, writing decoded
  // bytes to w as they arrive. Bytes on the result is nil.
  BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)
  ```
- Produces on `fakepool.SegmentBehavior`:
  ```go
  // ChunkSize splits a streamed payload into writes of this many bytes
  // (whole payload in one write when zero).
  ChunkSize int
  // TailGate, when non-nil, is waited on after the first chunk is written and
  // before the rest, so a test can prove bytes were served while the article
  // was provably still arriving. Closed by the test to release the tail.
  TailGate <-chan struct{}
  // FailAfterFirstChunk makes the first call to this message-ID write one
  // chunk and then return FailErr, modelling a connection lost mid-article.
  FailAfterFirstChunk bool
  ```
- `fakepool.Client.BodyStreamPriorityCalls() int64` counter.

- [ ] **Step 1: Write the failing fakepool test**

```go
package fakepool

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type recordingWriter struct {
	mu     chan struct{}
	writes [][]byte
}

func (r *recordingWriter) Write(p []byte) (int, error) {
	cp := append([]byte(nil), p...)
	r.writes = append(r.writes, cp)
	return len(p), nil
}

func TestBodyStreamPriorityChunksAndGates(t *testing.T) {
	c := New()
	payload := bytes.Repeat([]byte{7}, 10)
	gate := make(chan struct{})
	c.SetBehavior("a", SegmentBehavior{Bytes: payload, ChunkSize: 4, TailGate: gate})

	w := &recordingWriter{}
	done := make(chan error, 1)
	go func() { _, err := c.BodyStreamPriority(context.Background(), "a", w); done <- err }()

	// First chunk must land while the gate is closed.
	deadline := time.After(2 * time.Second)
	for len(w.writes) == 0 {
		select {
		case <-deadline:
			t.Fatal("no first chunk before the gate opened")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if len(w.writes) != 1 || len(w.writes[0]) != 4 {
		t.Fatalf("writes before gate = %v", w.writes)
	}
	close(gate)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	var all []byte
	for _, x := range w.writes {
		all = append(all, x...)
	}
	if !bytes.Equal(all, payload) {
		t.Fatalf("reassembled %v", all)
	}
	if c.BodyStreamPriorityCalls() != 1 {
		t.Fatal("call not counted")
	}
}

func TestBodyStreamPriorityFailAfterFirstChunk(t *testing.T) {
	c := New()
	payload := bytes.Repeat([]byte{9}, 8)
	c.SetBehavior("a", SegmentBehavior{Bytes: payload, ChunkSize: 4, FailAfterFirstChunk: true, FailErr: errors.New("conn died")})

	w := &recordingWriter{}
	_, err := c.BodyStreamPriority(context.Background(), "a", w)
	if err == nil || len(w.writes) != 1 {
		t.Fatalf("first call: err=%v writes=%d, want error after one chunk", err, len(w.writes))
	}
	w2 := &recordingWriter{}
	if _, err := c.BodyStreamPriority(context.Background(), "a", w2); err != nil {
		t.Fatalf("second call must succeed: %v", err)
	}
	if len(w2.writes) != 2 {
		t.Fatalf("second call writes = %d, want 2 full chunks", len(w2.writes))
	}
}
```

The `recordingWriter` above is read from the test goroutine while written from the fake's goroutine; guard `writes` with a `sync.Mutex` in the real test file (drop the unused `mu` channel field).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/testsupport/fakepool/ -run TestBodyStreamPriority -v`
Expected: FAIL, undefined method / fields.

- [ ] **Step 3: Implement in fakepool**

Add the three fields to `SegmentBehavior` with the comments from the Interfaces block. Add counter `bodyStreamPriCalls atomic.Int64` to `Client`, reset it in `ResetCounters`, and:

```go
// BodyStreamPriority satisfies pool.NntpClient. The payload is written to w
// in ChunkSize pieces (one write when zero), pausing on TailGate after the
// first chunk when set, and failing after the first chunk on the first call
// when FailAfterFirstChunk is set.
func (c *Client) BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	c.bodyStreamPriCalls.Add(1)
	c.countMessage(messageID)
	defer c.enter()()
	return c.serveBody(ctx, messageID, w, onMeta...)
}

func (c *Client) BodyStreamPriorityCalls() int64 { return c.bodyStreamPriCalls.Load() }
```

Replace the single `w.Write(payload)` in `serveBody` (line ~414) with:

```go
	payload := b.Bytes
	if w != nil && len(payload) > 0 {
		chunk := b.ChunkSize
		if chunk <= 0 || chunk > len(payload) {
			chunk = len(payload)
		}
		first := true
		for off := 0; off < len(payload); off += chunk {
			end := min(off+chunk, len(payload))
			if _, err := w.Write(payload[off:end]); err != nil {
				return nil, err
			}
			if first {
				first = false
				if b.FailAfterFirstChunk {
					if n, ok := c.perIDCalls.Load(messageID); ok && n.(*atomic.Int64).Load() == 1 {
						failErr := b.FailErr
						if failErr == nil {
							failErr = errTransientFakepool
						}
						return nil, failErr
					}
				}
				if b.TailGate != nil {
					select {
					case <-b.TailGate:
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				}
			}
		}
	}
```

Add to `pool.NntpClient` the method from the Interfaces block. Add to `multiRecordingClient` in `usenet_reader_storm_test.go`:

```go
func (r *multiRecordingClient) BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	r.record(messageID)
	return r.Client.BodyStreamPriority(ctx, messageID, w, onMeta...)
}
```

(Mirror whatever bookkeeping its `BodyPriority` does at lines 155-160; the name `record` is illustrative, copy the real statements.)

- [ ] **Step 4: Build everything and run the fakes**

Run: `go build ./... && go vet ./... && go test ./internal/testsupport/... ./internal/pool/ ./internal/usenet/ 2>&1 | tail -5`
Expected: PASS. Any other `pool.NntpClient` implementation the build reports gets the same one-line delegating method.

- [ ] **Step 5: Commit**

```bash
git add internal/pool/nntpclient.go internal/testsupport/fakepool/ internal/usenet/usenet_reader_storm_test.go
git commit -m "feat(pool): BodyStreamPriority on NntpClient with streaming test double"
```

---

### Task 3: Watermark segment

**Files:**
- Modify: `internal/usenet/segment.go:200-380`
- Test: `internal/usenet/segment_test.go` (append)

**Interfaces:**
- Produces (package-private):
  ```go
  // attemptWriter returns an io.Writer for one fetch attempt. Bytes written
  // are published to readers as they arrive; a later attempt's writer only
  // publishes once it has passed what earlier attempts already published.
  func (s *segment) attemptWriter() *segmentWriter
  // finish marks the segment complete with the bytes w received.
  func (s *segment) finish(w *segmentWriter)
  // published reports how many bytes readers may currently see.
  func (s *segment) published() int64
  // bytes returns the complete buffer after finish (nil before), for the cache.
  func (w *segmentWriter) bytes() []byte
  ```
- `SetData`, `SetError`, `Release`, `GetReaderContext`, `GetReader`, `DataLen`, `GetDownloadError`, `Close` keep their signatures. `GetReaderContext` now returns a reader that serves bytes progressively and blocks only for the bytes it needs.

- [ ] **Step 1: Write the failing tests**

```go
func TestSegmentServesBytesBeforeFinish(t *testing.T) {
	s := newSegment("id", 0, 9, 10, nil, 0)
	w := s.attemptWriter()
	_, _ = w.Write([]byte{1, 2, 3, 4})

	r := s.GetReaderContext(context.Background())
	buf := make([]byte, 3)
	n, err := r.Read(buf)
	if err != nil || n != 3 || !bytes.Equal(buf, []byte{1, 2, 3}) {
		t.Fatalf("read before finish: n=%d err=%v buf=%v", n, err, buf)
	}

	done := make(chan struct{})
	go func() {
		rest := make([]byte, 10)
		total := 0
		for total < 7 {
			nn, err := r.Read(rest[total:])
			total += nn
			if err != nil {
				t.Errorf("tail read: %v", err)
				break
			}
		}
		if !bytes.Equal(rest[:7], []byte{4, 5, 6, 7, 8, 9, 10}) {
			t.Errorf("tail = %v", rest[:7])
		}
		close(done)
	}()
	time.Sleep(20 * time.Millisecond) // reader must be parked, not erroring
	_, _ = w.Write([]byte{5, 6, 7, 8, 9, 10})
	s.finish(w)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("reader never got the tail")
	}
	if _, err := r.Read(make([]byte, 1)); err != io.EOF {
		t.Fatalf("after full range want EOF, got %v", err)
	}
}

func TestSegmentSecondAttemptDoesNotRewind(t *testing.T) {
	s := newSegment("id", 0, 7, 8, nil, 0)
	w1 := s.attemptWriter()
	_, _ = w1.Write([]byte{1, 2, 3, 4})
	if s.published() != 4 {
		t.Fatalf("published = %d", s.published())
	}
	// Attempt 1 dies; attempt 2 restarts from byte 0 with identical content.
	w2 := s.attemptWriter()
	_, _ = w2.Write([]byte{1, 2})
	if s.published() != 4 {
		t.Fatalf("a shorter second attempt must not rewind: %d", s.published())
	}
	_, _ = w2.Write([]byte{3, 4, 5, 6})
	if s.published() != 6 {
		t.Fatalf("second attempt past the watermark must publish: %d", s.published())
	}
	_, _ = w2.Write([]byte{7, 8})
	s.finish(w2)
	r := s.GetReaderContext(context.Background())
	got, err := io.ReadAll(r)
	if err != nil || !bytes.Equal(got, []byte{1, 2, 3, 4, 5, 6, 7, 8}) {
		t.Fatalf("got %v err %v", got, err)
	}
	if !bytes.Equal(w2.bytes(), got) {
		t.Fatal("bytes() must return the finished buffer")
	}
}

func TestSegmentTrimmedStartWaitsOnlyForItsFirstByte(t *testing.T) {
	s := newSegment("id", 5, 9, 10, nil, 0) // reader wants bytes 5..9
	w := s.attemptWriter()
	_, _ = w.Write([]byte{0, 1, 2, 3, 4, 5, 6})
	r := s.GetReaderContext(context.Background())
	buf := make([]byte, 2)
	n, err := r.Read(buf)
	if err != nil || n != 2 || !bytes.Equal(buf, []byte{5, 6}) {
		t.Fatalf("n=%d err=%v buf=%v", n, err, buf)
	}
}

func TestSegmentReleaseAndCancelUnblockProgressiveReader(t *testing.T) {
	s := newSegment("id", 0, 9, 10, nil, 0)
	w := s.attemptWriter()
	_, _ = w.Write([]byte{1})
	r := s.GetReaderContext(context.Background())
	_, _ = r.Read(make([]byte, 1))
	errc := make(chan error, 1)
	go func() { _, err := r.Read(make([]byte, 1)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	s.Release()
	if err := <-errc; !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("release must unblock with ErrClosedPipe, got %v", err)
	}

	s2 := newSegment("id2", 0, 9, 10, nil, 0)
	ctx, cancel := context.WithCancel(context.Background())
	r2 := s2.GetReaderContext(ctx)
	go func() { _, err := r2.Read(make([]byte, 1)); errc <- err }()
	time.Sleep(10 * time.Millisecond)
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel must unblock, got %v", err)
	}
}

func TestSegmentSetDataStillWorks(t *testing.T) {
	s := newSegment("id", 2, 5, 6, nil, 0)
	s.SetData([]byte{0, 1, 2, 3, 4, 5})
	got, err := io.ReadAll(s.GetReader())
	if err != nil || !bytes.Equal(got, []byte{2, 3, 4, 5}) {
		t.Fatalf("got %v err %v", got, err)
	}
	if s.DataLen() != 6 {
		t.Fatalf("DataLen = %d", s.DataLen())
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/usenet/ -run 'TestSegment(Serves|Second|Trimmed|Release|SetData)' -v`
Expected: FAIL (undefined `attemptWriter`/`finish`/`published`).

- [ ] **Step 3: Implement**

Replace the data-handoff fields and methods in `segment` with:

```go
	// Progressive handoff. buf holds whatever the current attempt has
	// decoded; ready is how much of it readers may see. A later attempt
	// starts a fresh buf and swaps it in only once it has passed ready, so a
	// reader that already consumed the prefix never sees it move.
	buf       []byte
	ready     int64
	done      bool
	dataErr   error
	attempt   int
	notify    chan struct{} // closed and replaced on every publish
	dataReady chan struct{} // closed once, on completion or error (kept for GetDownloadError callers)
	readyOnce sync.Once

	reader      *segmentReader
	readerReady bool
	mx          sync.Mutex
	released    bool
```

```go
func newSegment(id string, start, end, segmentSize int64, groups []string, loaderIdx int) *segment {
	return &segment{
		Id: id, Start: start, End: end, SegmentSize: segmentSize, groups: groups, loaderIdx: loaderIdx,
		notify:    make(chan struct{}),
		dataReady: make(chan struct{}),
	}
}

// segmentWriter is one fetch attempt's sink.
type segmentWriter struct {
	s       *segment
	attempt int
	buf     []byte
}

func (s *segment) attemptWriter() *segmentWriter {
	s.mx.Lock()
	defer s.mx.Unlock()
	s.attempt++
	return &segmentWriter{s: s, attempt: s.attempt, buf: make([]byte, 0, s.SegmentSize)}
}

func (w *segmentWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	w.s.publish(w)
	return len(p), nil
}

func (w *segmentWriter) bytes() []byte { return w.buf }

// publish makes w's bytes visible if w is the current attempt and has
// passed the watermark.
func (s *segment) publish(w *segmentWriter) {
	s.mx.Lock()
	if s.released || s.done || w.attempt != s.attempt || int64(len(w.buf)) <= s.ready {
		s.mx.Unlock()
		return
	}
	s.buf = w.buf
	s.ready = int64(len(w.buf))
	s.wakeLocked()
	s.mx.Unlock()
}

func (s *segment) wakeLocked() {
	close(s.notify)
	s.notify = make(chan struct{})
}

func (s *segment) finish(w *segmentWriter) {
	s.mx.Lock()
	if s.released || s.done || w.attempt != s.attempt {
		s.mx.Unlock()
		return
	}
	s.buf = w.buf
	s.ready = int64(len(w.buf))
	s.done = true
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

func (s *segment) published() int64 {
	s.mx.Lock()
	defer s.mx.Unlock()
	return s.ready
}

func (s *segment) SetData(data []byte) {
	if s == nil {
		return
	}
	s.mx.Lock()
	if s.released || s.done {
		s.mx.Unlock()
		return
	}
	s.buf = data
	s.ready = int64(len(data))
	s.done = true
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

func (s *segment) SetError(err error) {
	if s == nil || err == nil {
		return
	}
	s.mx.Lock()
	if s.dataErr == nil {
		s.dataErr = err
	}
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}

func (s *segment) DataLen() int {
	if s == nil {
		return 0
	}
	s.mx.Lock()
	defer s.mx.Unlock()
	return int(s.ready)
}

func (s *segment) Release() {
	if s == nil {
		return
	}
	s.mx.Lock()
	if s.released {
		s.mx.Unlock()
		return
	}
	s.released = true
	s.buf = nil
	if s.dataErr == nil {
		s.dataErr = io.ErrClosedPipe
	}
	s.wakeLocked()
	s.mx.Unlock()
	s.signalReady()
}
```

Reader:

```go
// segmentReader serves [Start, End] of the segment, waiting only for the
// bytes each Read needs.
type segmentReader struct {
	s   *segment
	ctx context.Context
	off int64 // absolute offset into the article
}

func (s *segment) GetReaderContext(ctx context.Context) io.Reader {
	s.mx.Lock()
	defer s.mx.Unlock()
	if s.reader == nil {
		s.reader = &segmentReader{s: s, off: s.Start}
	}
	s.reader.ctx = ctx
	return s.reader
}

func (s *segment) GetReader() io.Reader { return s.GetReaderContext(context.Background()) }

func (r *segmentReader) Read(p []byte) (int, error) {
	s := r.s
	if r.off > s.End {
		return 0, io.EOF
	}
	for {
		s.mx.Lock()
		if s.dataErr != nil && (s.released || s.ready <= r.off) {
			err := s.dataErr
			s.mx.Unlock()
			return 0, err
		}
		if s.ready > r.off {
			end := min(s.ready, s.End+1)
			n := copy(p, s.buf[r.off:end])
			r.off += int64(n)
			s.mx.Unlock()
			return n, nil
		}
		if s.done {
			s.mx.Unlock()
			return 0, io.ErrUnexpectedEOF
		}
		wait := s.notify
		s.mx.Unlock()
		select {
		case <-wait:
		case <-r.ctx.Done():
			return 0, r.ctx.Err()
		}
	}
}
```

Notes for the implementer: the previous `errorReader`/`limitedReader` fields go away; keep `GetDownloadError`, `signalReady`, `Close`, and `newSegment`'s other fields. `SetError` after bytes were served returns the error once the reader reaches the watermark, which is the same outcome as today's whole-article error, just later. A `SetError` racing a successful `finish` keeps the error (the fetch loop only calls one of them per attempt).

- [ ] **Step 4: Run the segment tests and the whole package under race**

Run: `go test -race ./internal/usenet/ 2>&1 | tail -5`
Expected: PASS, including the existing storm/lane/prefetch/sequential tests.

- [ ] **Step 5: Commit**

```bash
git add internal/usenet/segment.go internal/usenet/segment_test.go
git commit -m "feat(usenet): watermark segment buffer with progressive reader"
```

---

### Task 4: Stream fetches into the segment

**Files:**
- Modify: `internal/usenet/usenet_reader.go:457-600` (`downloadSegmentWithRetry`) and `:718-733` (goroutine tail)
- Test: `internal/usenet/usenet_reader_progressive_test.go`

**Interfaces:**
- `downloadSegmentWithRetry(ctx, seg) ([]byte, error)` keeps its signature. For priority readers it now streams via `cp.BodyStreamPriority(attemptCtx, seg.Id, w)` with `w := seg.attemptWriter()` per attempt, calls `seg.finish(w)` on success, and returns `w.bytes()` for the cache `Put`. For import readers (`b.priority == false`) behaviour is unchanged (`cp.Body`, `SetData` by the caller).
- The goroutine tail becomes: on `err == nil`, call `s.SetData(data)` only when `!b.priority` (progressive readers were finished inside the fetch); the hole/zero-fill branches are unchanged.

- [ ] **Step 1: Write the failing tests**

```go
package usenet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// The reader must hand over the head of an article while its tail is
// provably still on the wire. Gate-based so it cannot flake on timing.
func TestReaderServesHeadWhileTailIsHeld(t *testing.T) {
	ctx := context.Background()
	const segSize = 8 << 10
	fp := fakepool.New()
	gate := make(chan struct{})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024, TailGate: gate,
	})
	rg := buildEagerRange(ctx, t, 1, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, 1)
	ur.Start()

	buf := make([]byte, 512)
	readDone := make(chan error, 1)
	go func() { _, err := io.ReadFull(ur, buf); readDone <- err }()
	select {
	case err := <-readDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("first 512 bytes were not served while the tail was held")
	}
	if !bytes.Equal(buf, segments.Payload(0, segSize)[:512]) {
		t.Fatal("head bytes differ")
	}
	close(gate)
	rest, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(append(buf, rest...), segments.Payload(0, segSize)) {
		t.Fatal("full payload differs")
	}
}

// A connection lost after the first chunk must yield the exact payload once
// after the retry, never a duplicated prefix.
func TestReaderRetriesPartialDeliveryWithoutDuplicating(t *testing.T) {
	ctx := context.Background()
	const segSize = 4 << 10
	fp := fakepool.New()
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 1024,
		FailAfterFirstChunk: true, FailErr: errors.New("connection reset"),
	})
	rg := buildEagerRange(ctx, t, 1, segSize)
	ur := newReaderForTest(t, ctx, fp, rg, 1)
	ur.Start()
	got, err := io.ReadAll(ur)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, segments.Payload(0, segSize)) {
		t.Fatalf("got %d bytes, want %d exact", len(got), segSize)
	}
	if fp.BodyStreamPriorityCalls() != 2 {
		t.Fatalf("calls = %d, want 2 (one failed, one retry)", fp.BodyStreamPriorityCalls())
	}
}

// Import readers keep the buffered path.
func TestImportReaderStillUsesBufferedBody(t *testing.T) {
	ctx := context.Background()
	fp := fakepool.New()
	configurePool := func() {
		fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{Bytes: segments.Payload(0, 1024)})
	}
	configurePool()
	rg := buildEagerRange(ctx, t, 1, 1024)
	getter := func() (pool.NntpClient, error) { return fp, nil }
	ur, err := NewUsenetReader(ctx, getter, rg, 1, noopMetrics{}, "import", nil, WithImportProfile(nil))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ur.Close() })
	ur.Start()
	if _, err := io.ReadAll(ur); err != nil {
		t.Fatal(err)
	}
	if fp.BodyCalls() != 1 || fp.BodyStreamPriorityCalls() != 0 {
		t.Fatalf("import must use Body: body=%d stream=%d", fp.BodyCalls(), fp.BodyStreamPriorityCalls())
	}
}
```

Add the `pool` import. Check `WithImportProfile(nil)` compiles against `ConnBudget` (interface, nil is fine) and that `NewUsenetReader` accepts variadic `ReaderOption` (line 192; it does, `metadata_remote_file.go:2047` passes one).

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/usenet/ -run 'TestReader(ServesHead|RetriesPartial)|TestImportReaderStill' -v`
Expected: the first two FAIL (head test times out because bytes arrive only at the end; retry test sees `BodyStreamPriorityCalls == 0`).

- [ ] **Step 3: Implement**

Inside the `retry.Do` closure, replace the `if b.priority { ... } else { ... }` fetch and the success tail with:

```go
			var result *nntppool.ArticleBody
			var err error
			var w *segmentWriter
			if b.priority {
				// Streaming: priority lane, decoded bytes published to readers
				// as each wire read lands. A failed attempt leaves its bytes
				// visible; the next attempt starts a fresh buffer and only
				// publishes once it has passed what readers already saw.
				w = seg.attemptWriter()
				result, err = cp.BodyStreamPriority(attemptCtx, seg.Id, w)
			} else {
				// Import: normal lane, buffered — always yields to streaming reads.
				result, err = cp.Body(attemptCtx, seg.Id)
			}
			fetchDur := time.Since(fetchStart)
			if err != nil {
				// ... existing timeout log and corruption mapping unchanged ...
				return err
			}

			if w != nil {
				seg.finish(w)
				resultBytes = w.bytes()
			} else {
				resultBytes = result.Bytes
			}
			b.metricsTracker.IncArticlesDownloaded()
			b.metricsTracker.UpdateDownloadProgress(b.streamID, int64(len(resultBytes)))
			return nil
```

The `bytesWritten` used in the corruption error should come from `len(w.bytes())` when `w != nil`, else `result.BytesDecoded`.

In the download goroutine tail (line ~728), change `s.SetData(data)` to:

```go
			} else if !b.priority {
				s.SetData(data)
			}
```

- [ ] **Step 4: Run the package under race**

Run: `go test -race ./internal/usenet/ ./internal/nzbfilesystem/ 2>&1 | tail -5`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/usenet/
git commit -m "feat(usenet): stream priority fetches into the segment as bytes arrive"
```

---

### Task 5: End-to-end check through MetadataVirtualFile

**Files:**
- Test: `internal/nzbfilesystem/progressive_read_test.go`

- [ ] **Step 1: Write the test**

```go
package nzbfilesystem

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
)

// A player's first read must complete while the rest of the first article is
// still arriving.
func TestFirstByteArrivesBeforeArticleCompletes(t *testing.T) {
	ctx := context.Background()
	const n, segSize = 4, 64 << 10
	fp := fakepool.New()
	gate := make(chan struct{})
	configurePoolForFile(fp, n, segSize, fakepool.SegmentBehavior{ChunkSize: 4096})
	fp.SetBehavior(segments.MessageID(0), fakepool.SegmentBehavior{
		Bytes: segments.Payload(0, segSize), ChunkSize: 4096, TailGate: gate,
	})
	mvf := newTestMVF(t, ctx, fp, n, segSize, 2)

	buf := make([]byte, 1024)
	done := make(chan error, 1)
	go func() { _, err := mvf.ReadAt(buf, 0); done <- err }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadAt did not return while the article tail was held")
	}
	close(gate)
	rest := make([]byte, n*segSize-1024)
	if _, err := mvf.ReadAt(rest, 1024); err != nil {
		t.Fatal(err)
	}
	want := segments.FileBytes(n, segSize)
	if string(buf) != string(want[:1024]) || string(rest) != string(want[1024:]) {
		t.Fatal("streamed bytes differ from payload")
	}
}
```

- [ ] **Step 2: Run**

Run: `go test -race ./internal/nzbfilesystem/ -run TestFirstByteArrivesBeforeArticleCompletes -v`
Expected: PASS. If `ReadAt` returns only after the gate opens, the shared reader path is buffering somewhere above `UsenetReader` (check `tsRemuxReader`/`skipLimitReader` wrappers are not engaged for a plain file).

- [ ] **Step 3: Commit**

```bash
git add internal/nzbfilesystem/progressive_read_test.go
git commit -m "test(nzbfilesystem): first byte is served before the article completes"
```

---

### Task 6: Benchmark and live A/B

- [ ] **Step 1: Simulated**

Run: `make bench-stream && make bench-compare BASE=baseline-main`
Expected: B1 `ttfb_mean` drops well below baseline on both profiles (premium: from ~137 ms toward ~50 ms; slow-4m: from ~4 s toward well under 1 s); B2 throughput within tolerance; B3 `read_p50` drops similarly (each probe now waits for its byte, not the article); no REGRESSION rows. If B2 throughput regresses, profile `segmentWriter.Write` (a `close`+`make` per socket read is cheap, but `append` growth is not: confirm `attemptWriter` pre-sizes to `SegmentSize`).

- [ ] **Step 2: Live**

Run the dev server from this branch (`~/altmount-dev/run.sh`), then `~/altmount-dev/bench-live.sh progressive`. Expected: `ttfb_ms` and `seek_p50_ms` drop on all three files versus `bench-live-conn-hygiene.tsv`; `throughput_MBps` within provider noise.

---

### Task 7: Release nntppool and open the stacked PR

- [ ] **Step 1: Release nntppool v4.21.0**

```bash
cd ~/mio/nntppool && git checkout main && git merge --ff-only feat/body-stream-priority && git push origin main && git tag v4.21.0 && git push origin v4.21.0
```

If `main` cannot fast-forward, open a PR in nntppool instead and wait for it; do not rewrite history there.

- [ ] **Step 2: Drop the replace and pin the release**

```bash
cd /Users/javi/orca/workspaces/altmount/halibut
go mod edit -dropreplace github.com/javi11/nntppool/v4 && go get github.com/javi11/nntppool/v4@v4.21.0 && go mod tidy && go build ./... && go test -race ./internal/usenet/ ./internal/nzbfilesystem/ ./internal/pool/
git add go.mod go.sum && git commit -m "chore(deps): nntppool v4.21.0 for BodyStreamPriority"
```

- [ ] **Step 3: Push and open the PR**

```bash
git push -u origin feat/streaming-progressive-delivery
gh pr create --base feat/streaming-conn-hygiene --title "feat(usenet): progressive segment delivery" --body-file - <<'EOF'
## Summary
- A segment now publishes decoded bytes to readers as each wire read lands; a reader blocks only for the bytes it needs, not for the whole article.
- Second fetch attempts after a partial delivery write into a fresh buffer and publish only past what readers already saw, so a retried article is never duplicated or rewound.
- Streaming readers fetch through `BodyStreamPriority` (nntppool v4.21.0); import readers keep the buffered path.

## Benchmark vs feat/streaming-conn-hygiene
<paste `make bench-compare BASE=baseline-main` table>

## Live A/B
<paste bench-live-conn-hygiene.tsv vs bench-live-progressive.tsv>

## Test plan
- [ ] `go test -race ./internal/usenet/ ./internal/nzbfilesystem/ ./internal/pool/`
- [ ] gate-based progressive tests (head served while tail held; partial-then-retry exact bytes)
- [ ] `make bench-compare` no regressions; B1/B3 latencies down
- [ ] live: TTFB and seek p50 down on all three files
EOF
```

---

## Self-review

- **Spec coverage:** watermark + per-attempt writer (Task 3), `BodyStreamPriority` in nntppool and on the interface (Tasks 1-2), fetch path and retry semantics (Task 4), unchanged `segcache.Put`/`PatchLookup`/import path (Task 4 keeps `SetData` for those), end-to-end and benchmark evidence (Tasks 5-6), release without `replace` (Task 7).
- **Placeholders:** the two `<paste ...>` markers are PR-body fill-ins for measured tables; the mock-server helper name in Task 1 is explicitly to be resolved against the existing nntppool tests.
- **Type consistency:** `attemptWriter() *segmentWriter`, `finish(*segmentWriter)`, `published() int64`, `(*segmentWriter).bytes()` are used identically in Tasks 3-4; `SegmentBehavior.ChunkSize/TailGate/FailAfterFirstChunk` and `BodyStreamPriorityCalls()` identically in Tasks 2, 4, 5.
