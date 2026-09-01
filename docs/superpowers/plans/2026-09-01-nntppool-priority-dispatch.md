# nntppool Priority Dispatch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make nntppool's priority lane actually prioritise, keep a streaming BODY off connections that are already draining a BODY, and remove the lock contention in `StatMany`'s dispatch — without reducing throughput.

**Architecture:** Three surgical changes inside nntppool's per-connection dispatch. (1) `tryNextRequest` probes lanes in strict order instead of an unbiased `select`. (2) A new unbuffered group channel `hotIdleBodyCh` that a connection offers *only* while it has no body in flight (a `nil` channel otherwise, so it is never ready) — priority bodies are steered to free connections rather than capacity being reserved. (3) `StatMany`'s worker dispatch swaps an unbuffered channel for an atomic cursor. All three cost nothing when there is no priority traffic.

**Tech Stack:** Go 1.25+, `github.com/javi11/nntppool/v4` (local checkout `/Users/javi/mio/nntppool`, clean on `main` at `v4.18.0`), `github.com/mnightingale/rapidyenc`. Measurement rig lives in AltMount: `internal/testsupport/nntpserver` + `internal/pool/contention_bench_test.go`.

**Spec:** `docs/superpowers/specs/2026-09-01-priority-lane-steering-design.md`

## Global Constraints

- **No throughput regression.** Import MB/s and aggregate STAT/s must not fall. Every mechanism must cost nothing when no priority traffic exists. No static reservation of connections or capacity.
- **nntppool's existing suite stays green**: `nntp_test.go` (71 tests), `stat_test.go` (10), `client_test.go` (3). Run `make test-race` in `/Users/javi/mio/nntppool`.
- Public API of nntppool does not change. `StatMany` keeps its signature and its documented "results arrive out of order" contract.
- The `replace` directive added in Task 1 must **not** be committed to AltMount's `main`. It is removed in Task 5.
- Two repos are in play. Every step names which. AltMount worktree: `/Users/javi/orca/workspaces/altmount/oystercatcher` (branch `oystercatcher`). nntppool: `/Users/javi/mio/nntppool`.

## Spec deviation (deliberate)

The spec's phase 1a says the blocking select at `nntp.go:1060-1064` "gets the same treatment" as `tryNextRequest`. **This plan does not change it.** A blocking `select` cannot un-receive a request, so it cannot be reordered without stashing an already-received request. More importantly it only runs when `tryNextRequest` just found *both* lanes empty, so a coin flip there decides between two requests that arrived essentially simultaneously into an idle connection — not the systematic bias D1 describes. Task 3 does add one case to that select (the idle-body channel), but the ordering is left alone.

## File Structure

**nntppool** (`/Users/javi/mio/nntppool`):

| File | Responsibility | Change |
|---|---|---|
| `nntp.go` | connection lifecycle, dispatch, group channels | Modify: `tryNextRequest`, `NNTPConnection` fields, `newNNTPConnectionFromConn`, `runConnSlot`, `providerGroup` struct + construction, `tryGroupResilient` |
| `stat.go` | `StatMany` dispatch | Modify: worker loop |
| `priority_test.go` | lane-ordering + steering tests | Create |
| `stat_test.go` | `StatMany` behaviour | Modify: add completeness/cancellation tests + dispatch benchmark |

**AltMount** (`/Users/javi/orca/workspaces/altmount/oystercatcher`): no source changes in this plan. Task 1 commits the already-written measurement rig; Task 5 bumps the dependency.

---

### Task 1: Land the measurement rig and capture the baseline

Every later gate compares against numbers this task produces. Nothing else can be verified until it exists.

**Files (AltMount):**
- Commit (already written, currently untracked): `internal/testsupport/nntpserver/nntpserver.go`, `internal/testsupport/nntpserver/nntpserver_test.go`, `internal/pool/contention_bench_test.go`, `go.mod`
- Modify: `go.mod` (temporary `replace`, removed in Task 5)

**Interfaces:**
- Consumes: nothing.
- Produces: `nntpserver.New(nntpserver.Config) (*Server, error)` with `Server.Dial(ctx) (net.Conn, error)` satisfying `nntppool.ConnFactory`, `Server.Counters() Counters`, `Server.ResetPeakInflight()`. Benchmarks `BenchmarkContention`, `BenchmarkContentionStatInflight`, `BenchmarkContentionHealthConcurrency` in package `pool`. Baseline file `/tmp/bench-baseline.txt`.

- [ ] **Step 1: Verify the rig builds and its own tests pass (AltMount)**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go build ./... && go vet ./internal/testsupport/nntpserver/ ./internal/pool/
go test ./internal/testsupport/nntpserver/ -race -count=1
```

Expected: `ok github.com/javi11/altmount/internal/testsupport/nntpserver`

- [ ] **Step 2: Commit the rig (AltMount)**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
git add internal/testsupport/nntpserver/ internal/pool/contention_bench_test.go go.mod go.sum
git commit -m "test(pool): add in-process NNTP wire server and contention benchmarks"
```

- [ ] **Step 3: Point AltMount at the local nntppool**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go mod edit -replace github.com/javi11/nntppool/v4=/Users/javi/mio/nntppool
go build ./...
```

Expected: builds clean. Do **not** commit this edit.

- [ ] **Step 4: Capture the baseline**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go test ./internal/pool/ -run '^$' -bench 'BenchmarkContention$' \
  -benchtime 1x -count=3 -timeout 60m 2>&1 \
  | grep -v "INFO\|^2026" > /tmp/bench-baseline.txt
tail -20 /tmp/bench-baseline.txt
```

Expected (medians, from the spec's Problem table — confirm they reproduce within ~5 %):

| Scenario | p50 | p95 | p99 | stream MB/s | import MB/s | STAT/s |
|---|---|---|---|---|---|---|
| S1 stream only | 146 | 156 | 158 | 39.8 | — | — |
| S2 stream + import | 143 | 201 | 210 | 37.6 | 458 | 561 |
| S3 stream + import + health | 153 | 202 | 217 | 36.5 | 445 | 1 111 |
| S4 import + health, no stream | — | — | — | — | 464 | 26 575 |
| S5 health only, no stream | — | — | — | — | — | 1 947 |

Also required: `dispatch_timeouts` and `other_errors` must both be `0` in every row. If they are not, stop — the rig is wrong and no gate below means anything.

- [ ] **Step 5: Capture the baseline CPU profile (for Task 4's gate)**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
ALTMOUNT_BENCH_DURATION=12s go test ./internal/pool/ -run '^$' \
  -bench 'BenchmarkContention$/import\+health_nostream' -benchtime 1x \
  -cpuprofile /tmp/cpu-baseline.prof -o /tmp/pool.test -timeout 10m > /dev/null 2>&1
go tool pprof -top -cum -nodecount=400 /tmp/pool.test /tmp/cpu-baseline.prof 2>/dev/null \
  | grep -E "runtime\.(lock2|selectgo)" | tee /tmp/lock-baseline.txt
```

Expected: two lines, roughly `runtime.lock2 ... 16.4%` and `runtime.selectgo ... 10.7%`. Record the exact percentages.

---

### Task 2: Strict lane ordering (spec phase 1a)

**Files (nntppool):**
- Modify: `nntp.go:457-476` (`tryNextRequest`)
- Test: `priority_test.go` (create)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `(*NNTPConnection).tryNextRequest() (req *Request, ok, got bool)` — unchanged signature, now probing `hotPrioCh` → `prioCh` → `hotReqCh` → `reqCh` in strict order.

**Why the test looks like this:** `tryNextRequest` is an unexported method on an unexported-ish type in package `nntppool`, so the test is in-package and drives it directly — no sockets, no goroutines, no timing. The connection's channel fields are declared as receive-only (`<-chan *Request`), which accepts a *buffered* channel, so every case is deterministic. The bias between `prioCh` and `reqCh` is probabilistic in the current code, so that test runs 100 iterations: pre-fix it passes with probability 2⁻¹⁰⁰; post-fix it always passes.

- [ ] **Step 1: Write the failing tests**

Create `/Users/javi/mio/nntppool/priority_test.go`:

```go
package nntppool

import "testing"

// newLaneTestConn builds a bare NNTPConnection wired only with request
// channels. Nothing else is touched, because tryNextRequest reads nothing else.
func newLaneTestConn(hotPrio, prio, hotReq, req chan *Request) *NNTPConnection {
	return &NNTPConnection{
		hotPrioCh: hotPrio,
		prioCh:    prio,
		hotReqCh:  hotReq,
		reqCh:     req,
	}
}

func TestTryNextRequest_PriorityBeatsNormal(t *testing.T) {
	// The bias is a coin flip today, so a single trial proves nothing.
	// 100 trials make a pre-fix pass astronomically unlikely.
	for i := range 100 {
		prio := make(chan *Request, 1)
		req := make(chan *Request, 1)
		prioReq := &Request{Payload: []byte("BODY <prio>\r\n")}
		normReq := &Request{Payload: []byte("BODY <norm>\r\n")}
		prio <- prioReq
		req <- normReq

		c := newLaneTestConn(nil, prio, nil, req)
		got, ok, found := c.tryNextRequest()
		if !found || !ok {
			t.Fatalf("iteration %d: found=%v ok=%v, want both true", i, found, ok)
		}
		if got != prioReq {
			t.Fatalf("iteration %d: got the normal-lane request, want the priority one", i)
		}
	}
}

func TestTryNextRequest_ColdPriorityBeatsHotNormal(t *testing.T) {
	// A priority request bound for a cold connection must outrank a normal
	// request bound for a hot one. Today hotReqCh is probed first.
	prio := make(chan *Request, 1)
	hotReq := make(chan *Request, 1)
	prioReq := &Request{Payload: []byte("BODY <prio>\r\n")}
	hotReq <- &Request{Payload: []byte("BODY <hotnorm>\r\n")}
	prio <- prioReq

	c := newLaneTestConn(nil, prio, hotReq, nil)
	got, ok, found := c.tryNextRequest()
	if !found || !ok {
		t.Fatalf("found=%v ok=%v, want both true", found, ok)
	}
	if got != prioReq {
		t.Fatal("hot normal request outranked a priority request")
	}
}

func TestTryNextRequest_HotPriorityFirst(t *testing.T) {
	hotPrio := make(chan *Request, 1)
	prio := make(chan *Request, 1)
	hotPrioReq := &Request{Payload: []byte("BODY <hotprio>\r\n")}
	hotPrio <- hotPrioReq
	prio <- &Request{Payload: []byte("BODY <coldprio>\r\n")}

	c := newLaneTestConn(hotPrio, prio, nil, nil)
	got, _, found := c.tryNextRequest()
	if !found || got != hotPrioReq {
		t.Fatal("hot priority channel must be probed before the cold one")
	}
}

func TestTryNextRequest_EmptyReturnsNotGot(t *testing.T) {
	c := newLaneTestConn(nil, nil, nil, nil)
	if _, _, found := c.tryNextRequest(); found {
		t.Fatal("all-nil channels must report got=false")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestTryNextRequest' -v ./...
```

Expected: `TestTryNextRequest_ColdPriorityBeatsHotNormal` FAILs with "hot normal request outranked a priority request". `TestTryNextRequest_PriorityBeatsNormal` FAILs with "got the normal-lane request" (at some iteration). The other two PASS.

- [ ] **Step 3: Rewrite `tryNextRequest`**

In `/Users/javi/mio/nntppool/nntp.go`, replace the body of `tryNextRequest` (currently lines 457-476) and its doc comment:

```go
// tryNextRequest performs a non-blocking receive across the request channels in
// strict preference order: hot priority, cold priority, hot normal, cold normal.
// Priority always outranks normal — including a cold priority request over a hot
// normal one, because a stream waiting on a body cares far more about being
// dispatched at all than about landing on an already-warm connection.
//
// Each lane is its own non-blocking select rather than one combined select,
// because Go chooses uniformly among ready cases: a single select over prioCh
// and reqCh makes "priority" a coin flip exactly when both lanes are busy.
// Receives from nil channels are never ready, so the standalone path (prioCh and
// the hot channels nil) probes reqCh alone.
//
// got reports whether any channel was ready; ok is false when the channel that
// fired was closed.
func (c *NNTPConnection) tryNextRequest() (req *Request, ok, got bool) {
	select {
	case req, ok = <-c.hotPrioCh:
		return req, ok, true
	default:
	}
	select {
	case req, ok = <-c.prioCh:
		return req, ok, true
	default:
	}
	select {
	case req, ok = <-c.hotReqCh:
		return req, ok, true
	default:
	}
	select {
	case req, ok = <-c.reqCh:
		return req, ok, true
	default:
	}
	return nil, false, false
}
```

- [ ] **Step 4: Run the tests to verify they pass**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestTryNextRequest' -v ./...
```

Expected: all four PASS.

- [ ] **Step 5: Run the full suite**

```bash
cd /Users/javi/mio/nntppool
make test-race
```

Expected: `ok github.com/javi11/nntppool/v4`. If any existing test fails, it is asserting the old coin-flip behaviour — read it before changing it, and report rather than editing it to match.

- [ ] **Step 6: Commit**

```bash
cd /Users/javi/mio/nntppool
git add nntp.go priority_test.go
git commit -m "fix(dispatch): probe request lanes in strict priority order

A single select over prioCh and reqCh chooses uniformly, so the priority
lane degraded to a coin flip exactly when both lanes were busy. hotReqCh
was also probed before prioCh, letting a normal request bound for a hot
connection outrank a priority one."
```

---

### Task 3: Body-free steering (spec phase 1b)

A priority BODY only escapes a ~94 ms wait if it lands on a connection with no body in flight, because `readerLoop` must drain a whole body before parsing the next reply. This task adds a channel that only body-free connections listen on.

**Files (nntppool):**
- Modify: `nntp.go` — `NNTPConnection` struct (~line 235-239), `newNNTPConnectionFromConn` (line 296), `runConnSlot` (line 688 signature, ~line 809 assignment), the writer's blocking select (~line 1060), `tryNextRequest` (from Task 2), `providerGroup` struct (~line 1602-1606), group construction (~line 1883-1886), `runConnSlot` call site (~line 1956), `tryGroupResilient` dispatch (~line 2373-2400)
- Test: `priority_test.go` (extend)

**Interfaces:**
- Consumes: `(*NNTPConnection).tryNextRequest()` from Task 2.
- Produces: `providerGroup.hotIdleBodyCh chan *Request` (unbuffered); `NNTPConnection.hotIdleBodyCh <-chan *Request`; `(*NNTPConnection).idleBodyChan() <-chan *Request` returning the channel when `len(c.bodySem) == 0` and `nil` otherwise. `runConnSlot` gains a `hotIdleBodyCh <-chan *Request` parameter after `hotPrioCh`.

**Why `len(c.bodySem)` is the right signal:** the slot is released in `readerLoop` at `nntp.go:1432-1434`, *after* the full body has been read and delivered. The window where the writer has taken a slot but the reader has not started counts as busy, which errs toward "don't send a stream body here" — the safe direction.

- [ ] **Step 1: Write the failing test**

First **replace** the import block at the top of `priority_test.go` (Task 2 left it
as a bare `import "testing"`) with:

```go
import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)
```

Then append:

```go
// bodySteeringServer answers BODY with a small yEnc article and records which
// connection index served each message-id. The id in slowID blocks until
// release is closed, holding that connection's reader busy.
type bodySteeringServer struct {
	mu       sync.Mutex
	servedBy map[string]int // message-id -> connection index
	conns    int
	slowID   string
	release  chan struct{}
	started  chan struct{} // closed once slowID has reached the server
}

func (s *bodySteeringServer) factory(t *testing.T) ConnFactory {
	t.Helper()
	return func(ctx context.Context) (net.Conn, error) {
		client, server := net.Pipe()
		s.mu.Lock()
		idx := s.conns
		s.conns++
		s.mu.Unlock()

		go func() {
			defer func() { _ = server.Close() }()
			_, _ = server.Write([]byte("200 ready\r\n"))
			buf := make([]byte, 4096)
			for {
				n, err := server.Read(buf)
				if err != nil {
					return
				}
				cmd := strings.TrimRight(string(buf[:n]), "\r\n")
				if strings.HasPrefix(cmd, "DATE") {
					_, _ = server.Write([]byte("111 20240101000000\r\n"))
					continue
				}
				if !strings.HasPrefix(cmd, "BODY ") {
					_, _ = server.Write([]byte("500 unsupported\r\n"))
					continue
				}
				id := strings.Trim(strings.TrimPrefix(cmd, "BODY "), "<>")
				s.mu.Lock()
				s.servedBy[id] = idx
				s.mu.Unlock()

				if id == s.slowID {
					close(s.started)  // the connection is now genuinely busy
					<-s.release       // hold its reader until the test releases it
				}
				_, _ = server.Write(yencSinglePart([]byte("payload"), "f.bin"))
			}
		}()
		return client, nil
	}
}

// TestPriorityBodyAvoidsBusyConnection pins the property phase 1b exists for:
// while one connection is draining a body, a priority body must be steered to a
// connection that is free, not queued behind the in-flight one.
func TestPriorityBodyAvoidsBusyConnection(t *testing.T) {
	srv := &bodySteeringServer{
		servedBy: map[string]int{},
		slowID:   "slow@h",
		release:  make(chan struct{}),
		started:  make(chan struct{}),
	}
	c, err := NewClient(context.Background(), []Provider{{
		Factory:        srv.factory(t),
		Connections:    2,
		MinConnections: 2, // pre-warm both so neither has to dial mid-test
		Inflight:       1,
		SkipPing:       true,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Occupy one connection with a normal-lane body that will not complete.
	slowDone := make(chan struct{})
	go func() {
		defer close(slowDone)
		_, _ = c.Body(ctx, "slow@h")
	}()

	// Block on a signal rather than polling: the test must not proceed until a
	// connection is genuinely busy, and a sleep would make that a race.
	select {
	case <-srv.started:
	case <-time.After(10 * time.Second):
		t.Fatal("slow body never reached the server")
	}

	if _, err := c.BodyPriority(ctx, "fast@h"); err != nil {
		t.Fatalf("priority body: %v", err)
	}

	srv.mu.Lock()
	slowConn, fastConn := srv.servedBy["slow@h"], srv.servedBy["fast@h"]
	srv.mu.Unlock()

	if slowConn == fastConn {
		t.Fatalf("priority body landed on connection %d, which was already draining a body", fastConn)
	}

	close(srv.release)
	<-slowDone
}

func TestIdleBodyChanNilWhenBusy(t *testing.T) {
	hotIdle := make(chan *Request, 1)
	c := &NNTPConnection{
		hotIdleBodyCh: hotIdle,
		bodySem:       make(chan struct{}, 1),
	}
	if c.idleBodyChan() == nil {
		t.Fatal("a body-free connection must offer its idle-body channel")
	}
	c.bodySem <- struct{}{} // now a body is in flight
	if c.idleBodyChan() != nil {
		t.Fatal("a busy connection must not offer its idle-body channel")
	}
}
```

Note: `yencSinglePart` already exists in `testhelper_test.go:48`. Do not redefine it.

- [ ] **Step 2: Run the tests to verify they fail**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestPriorityBodyAvoidsBusyConnection|TestIdleBodyChanNilWhenBusy' -v ./...
```

Expected: compile error — `hotIdleBodyCh` and `idleBodyChan` do not exist yet. That counts as the failing state; the next steps make it compile and pass.

- [ ] **Step 3: Add the channel to the connection**

In `nntp.go`, in the `NNTPConnection` struct field block (currently lines 235-239), add after `hotPrioCh`:

```go
	hotPrioCh     <-chan *Request // unbuffered; set by runConnSlot before Run()
	hotIdleBodyCh <-chan *Request // unbuffered; read ONLY while this connection has no body in flight
```

Then add this method next to `tryNextRequest`:

```go
// idleBodyChan returns the group's idle-body channel while this connection has
// no body-bearing request outstanding, and nil otherwise. A nil channel is never
// ready in a select, so a connection whose reader is busy draining a body simply
// does not compete for priority bodies — which is the whole point: NNTP replies
// are FIFO per connection, so a priority body queued behind a 750 KB article
// waits for the entire transfer.
//
// len(bodySem) is the correct signal because readerLoop releases the slot only
// after the full body has been read and delivered. The window where the writer
// holds a slot but the reader has not started counts as busy — the safe side.
func (c *NNTPConnection) idleBodyChan() <-chan *Request {
	if len(c.bodySem) != 0 {
		return nil
	}
	return c.hotIdleBodyCh
}
```

- [ ] **Step 4: Probe the idle-body channel in `tryNextRequest`**

In `tryNextRequest` (rewritten in Task 2), insert this as the **first** probe, before the `hotPrioCh` case:

```go
	select {
	case req, ok = <-c.idleBodyChan():
		return req, ok, true
	default:
	}
```

- [ ] **Step 5: Add the channel to the writer's blocking select**

In `nntp.go`, in the writer's blocking select (currently lines 1060-1064), add one case at the top:

```go
			select {
			case req, ok = <-c.idleBodyChan():
			case req, ok = <-c.hotPrioCh:
			case req, ok = <-c.hotReqCh:
			case req, ok = <-c.prioCh:
			case req, ok = <-c.reqCh:
```

Leave the rest of that select (the `ctx.Done()`, `idleCh`, `keepaliveCh` cases) untouched, and do not reorder the existing cases — see "Spec deviation" above.

- [ ] **Step 6: Add the channel to the provider group**

In the `providerGroup` struct (currently lines 1602-1606), after `hotPrioCh`:

```go
	hotPrioCh     chan *Request // unbuffered; hot priority connections read this
	hotIdleBodyCh chan *Request // unbuffered; only connections with no body in flight read this
```

In the group construction literal (currently lines 1883-1886), after `hotPrioCh`:

```go
		hotPrioCh:     make(chan *Request),
		hotIdleBodyCh: make(chan *Request),
```

- [ ] **Step 7: Thread it through `runConnSlot`**

Add a parameter to `runConnSlot` (line 688) immediately after `hotPrioCh`:

```go
func runConnSlot(ctx context.Context, reqCh <-chan *Request, prioCh <-chan *Request, hotReqCh <-chan *Request, hotPrioCh <-chan *Request, hotIdleBodyCh <-chan *Request, factory ConnFactory, inflight int, statInflight int, auth Auth, userAgent string, idleTimeout time.Duration, stallTimeout time.Duration, keepaliveInterval time.Duration, keepaliveCommand string, gate *connGate, stats *providerStats, providerName string, wg *sync.WaitGroup, preWarm bool) {
```

Next to the existing assignments (currently lines 809-810):

```go
		nc.hotReqCh = hotReqCh
		nc.hotPrioCh = hotPrioCh
		nc.hotIdleBodyCh = hotIdleBodyCh
```

Update the call site (currently line 1956):

```go
		go runConnSlot(gctx, g.reqCh, g.prioCh, g.hotReqCh, g.hotPrioCh, g.hotIdleBodyCh, factory, inflight, statInflight, p.Auth, p.UserAgent, idleTimeout, stall, kaInterval, kaCmd, gate, &g.stats, name, &c.wg, preWarm)
```

- [ ] **Step 8: Steer priority bodies on the send side**

In `tryGroupResilient` (currently lines 2373-2400), replace the dispatch block. Before:

```go
	select {
	case hotCh <- req:
	default:
		select {
		case <-c.ctx.Done():
			return Response{}, false, true
		case <-reqCtx.Done():
			return Response{}, false, ctx.Err() != nil
		case <-g.ctx.Done():
			return Response{}, false, false
		case <-timer.C:
			return Response{Err: &AttemptTimeoutError{Provider: g.name, Timeout: attemptTimeout, Phase: PhaseDispatch}}, false, false
		case coldCh <- req:
		}
	}
```

After:

```go
	// A priority BODY prefers a connection with no body in flight. NNTP replies
	// are FIFO per connection, so landing behind an in-flight article costs the
	// whole transfer. Only body-free connections read hotIdleBodyCh, and only
	// while they stay body-free, so nothing is reserved: when no priority body
	// is in play nothing is ever sent here and every connection serves the
	// normal lane exactly as before.
	dispatched := false
	if priority && !isCheapCommand(payload) {
		select {
		case g.hotIdleBodyCh <- req:
			dispatched = true
		default:
		}
	}

	if !dispatched {
		select {
		case hotCh <- req:
		default:
			select {
			case <-c.ctx.Done():
				return Response{}, false, true
			case <-reqCtx.Done():
				return Response{}, false, ctx.Err() != nil
			case <-g.ctx.Done():
				return Response{}, false, false
			case <-timer.C:
				return Response{Err: &AttemptTimeoutError{Provider: g.name, Timeout: attemptTimeout, Phase: PhaseDispatch}}, false, false
			case coldCh <- req:
			}
		}
	}
```

- [ ] **Step 9: Run the tests to verify they pass**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestPriorityBodyAvoidsBusyConnection|TestIdleBodyChanNilWhenBusy|TestTryNextRequest' -race -v ./...
```

Expected: all PASS.

- [ ] **Step 10: Run the full suite**

```bash
cd /Users/javi/mio/nntppool
make test-race
```

Expected: `ok`. Every `runConnSlot` caller must have been updated — a missed one is a compile error, not a silent bug.

- [ ] **Step 11: Commit**

```bash
cd /Users/javi/mio/nntppool
git add nntp.go priority_test.go
git commit -m "feat(dispatch): steer priority bodies to connections with no body in flight

NNTP replies are FIFO per connection, so a priority BODY queued behind an
in-flight article waits for the entire transfer. Connections now offer an
idle-body channel only while bodySem is empty (nil otherwise, so never
ready), and priority body sends try it first. Nothing is reserved: with no
priority traffic the channel is never written and dispatch is unchanged."
```

---

### Task 4: Lock-free STAT dispatch (spec phase 1c)

**Files (nntppool):**
- Modify: `stat.go:112-145` (the `StatMany` worker goroutine)
- Test: `stat_test.go` (extend)

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `(*Client).StatMany(ctx, messageIDs, opts) <-chan StatManyResult` — signature and contract unchanged.

**Why:** the current dispatch feeds up to `conc` (4 096) workers from one unbuffered channel. Every send takes `hchan.lock` and wakes one goroutine out of a 4 096-deep receive queue. The work is a fully-materialised slice, so a channel is the streaming primitive applied to non-streaming work.

- [ ] **Step 1: Write the failing tests**

Append to `/Users/javi/mio/nntppool/stat_test.go`:

```go
// TestStatMany_CompletenessUnderWideConcurrency pins the property the dispatch
// rewrite must preserve: every id yields exactly one result, with no duplicates
// and none dropped, even when workers vastly outnumber the ids they share.
func TestStatMany_CompletenessUnderWideConcurrency(t *testing.T) {
	const n = 500
	ids := make([]string, n)
	replies := make(map[string]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("m%04d@h", i)
		replies[ids[i]] = fmt.Sprintf("223 %d <%s> exists", i, ids[i])
	}

	c, err := NewClient(context.Background(), []Provider{{
		Factory:     makeStatByIDFactory(t, nil, nil, replies),
		Connections: 4,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	seen := make(map[string]int, n)
	for res := range c.StatMany(context.Background(), ids, StatManyOptions{Concurrency: 256}) {
		seen[res.MessageID]++
	}

	if len(seen) != n {
		t.Fatalf("got %d distinct results, want %d", len(seen), n)
	}
	for id, count := range seen {
		if count != 1 {
			t.Errorf("%s: %d results, want exactly 1", id, count)
		}
	}
}

// TestStatMany_CancelMidSweepClosesChannel pins the documented contract: on
// cancellation dispatch stops, in-flight checks are cancelled, and the channel
// is closed, so a caller ranging over it always terminates.
func TestStatMany_CancelMidSweepClosesChannel(t *testing.T) {
	const n = 2000
	ids := make([]string, n)
	replies := make(map[string]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("c%04d@h", i)
		replies[ids[i]] = fmt.Sprintf("223 %d <%s> exists", i, ids[i])
	}

	c, err := NewClient(context.Background(), []Provider{{
		Factory:     makeStatByIDFactory(t, nil, nil, replies),
		Connections: 2,
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	ctx, cancel := context.WithCancel(context.Background())
	ch := c.StatMany(ctx, ids, StatManyOptions{Concurrency: 64})

	got := 0
	for range ch {
		got++
		if got == 10 {
			cancel()
		}
	}
	// Reaching here at all is the assertion: the range terminated, so the
	// channel was closed rather than leaked.
	if got >= n {
		t.Fatalf("drained all %d results despite cancelling after 10", n)
	}
	if ctx.Err() == nil {
		t.Fatal("context should be cancelled")
	}
}

// BenchmarkStatManyDispatch isolates dispatch cost: every STAT is answered
// immediately, so what it measures is the hand-off machinery, not the wire.
func BenchmarkStatManyDispatch(b *testing.B) {
	const n = 4096
	ids := make([]string, n)
	replies := make(map[string]string, n)
	for i := range ids {
		ids[i] = fmt.Sprintf("b%04d@h", i)
		replies[ids[i]] = fmt.Sprintf("223 %d <%s> exists", i, ids[i])
	}

	c, err := NewClient(context.Background(), []Provider{{
		Factory:     makeStatByIDFactory(nil, nil, nil, replies),
		Connections: 8,
	}})
	if err != nil {
		b.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	b.ResetTimer()
	for range b.N {
		for range c.StatMany(context.Background(), ids, StatManyOptions{Concurrency: n}) {
		}
	}
}
```

`makeStatByIDFactory` takes a `*testing.T` only to call `t.Helper()`; for the benchmark, change its first parameter to `testing.TB` in `stat_test.go:18` and pass `nil` — `t.Helper()` must then be guarded:

```go
func makeStatByIDFactory(t testing.TB, mu *sync.Mutex, cmdLog *[]string, replies map[string]string) ConnFactory {
	if t != nil {
		t.Helper()
	}
```

- [ ] **Step 2: Run the tests to verify the state before the change**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestStatMany_CompletenessUnderWideConcurrency|TestStatMany_CancelMidSweepClosesChannel' -race -v ./...
go test -run '^$' -bench BenchmarkStatManyDispatch -benchtime 20x ./... | tee /tmp/statdispatch-before.txt
```

Expected: both tests PASS (they pin behaviour that already holds — they are the regression net for the rewrite, not a red test). Record the benchmark's ns/op; Step 5 must improve it.

- [ ] **Step 3: Replace the channel dispatch with an atomic cursor**

In `/Users/javi/mio/nntppool/stat.go`, replace the worker-pool block and the dispatch loop (currently lines 112-145 inside the `go func()`). Before:

```go
		ids := make(chan string)
		var wg sync.WaitGroup
		wg.Add(conc)
		for range conc {
			go func() {
				defer wg.Done()
				for id := range ids {
					res := c.statOne(ctx, id, target, targetErr, opts.Priority)
					select {
					case out <- res:
					case <-ctx.Done():
						return
					}
				}
			}()
		}

	dispatch:
		for _, id := range messageIDs {
			select {
			case <-ctx.Done():
				break dispatch
			case ids <- id:
			}
		}
		close(ids)

		wg.Wait()
```

After:

```go
		// Workers pull their own index instead of being fed by a channel. The
		// work is a fully-materialised slice, so a channel buys nothing and
		// costs a great deal: with conc up to maxStatConcurrency, every send
		// takes hchan.lock and wakes one goroutine out of a receive queue
		// thousands deep. One uncontended atomic add replaces that hand-off.
		//
		// Worker count stays conc because statOne blocks until its reply
		// arrives, so one goroutine per outstanding STAT is inherent.
		var cursor atomic.Int64
		var wg sync.WaitGroup
		wg.Add(conc)
		for range conc {
			go func() {
				defer wg.Done()
				for {
					i := int(cursor.Add(1)) - 1
					if i >= len(messageIDs) {
						return
					}
					if ctx.Err() != nil {
						return
					}
					res := c.statOne(ctx, messageIDs[i], target, targetErr, opts.Priority)
					select {
					case out <- res:
					case <-ctx.Done():
						return
					}
				}
			}()
		}

		wg.Wait()
```

Add `"sync/atomic"` to the imports in `stat.go`.

- [ ] **Step 4: Run the tests to verify they still pass**

```bash
cd /Users/javi/mio/nntppool
go test -run 'TestStatMany' -race -v ./...
```

Expected: every `TestStatMany*` test PASSes, including the pre-existing ones in `stat_test.go` and `stat_pipeline_test.go` / `stat_coalesce_test.go`.

- [ ] **Step 5: Verify the dispatch benchmark improved**

```bash
cd /Users/javi/mio/nntppool
go test -run '^$' -bench BenchmarkStatManyDispatch -benchtime 20x ./... | tee /tmp/statdispatch-after.txt
diff /tmp/statdispatch-before.txt /tmp/statdispatch-after.txt
```

Expected: ns/op lower than Step 2's. If it is not, stop and report — the rewrite is not paying for itself and the premise needs re-examining.

- [ ] **Step 6: Run the full suite**

```bash
cd /Users/javi/mio/nntppool
make test-race
```

Expected: `ok`.

- [ ] **Step 7: Commit**

```bash
cd /Users/javi/mio/nntppool
git add stat.go stat_test.go
git commit -m "perf(stat): dispatch StatMany workers from an atomic cursor

Feeding up to maxStatConcurrency workers from one unbuffered channel made
every id a locked hand-off waking one goroutine out of a receive queue
thousands deep. The ids are a materialised slice, so workers can pull their
own index with a single atomic add."
```

---

### Task 5: Gate the whole phase and release

**Files:**
- Modify (nntppool): none — tag only
- Modify (AltMount): `go.mod`, `go.sum`

**Interfaces:**
- Consumes: everything from Tasks 1-4.
- Produces: nntppool `v4.19.0`; AltMount depending on it with no `replace`.

- [ ] **Step 1: Re-run the contention benchmark against the modified nntppool**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go test ./internal/pool/ -run '^$' -bench 'BenchmarkContention$' \
  -benchtime 1x -count=3 -timeout 60m 2>&1 \
  | grep -v "INFO\|^2026" > /tmp/bench-after.txt
```

- [ ] **Step 2: Compare against the baseline and apply the gate**

```bash
diff /tmp/bench-baseline.txt /tmp/bench-after.txt
```

Gate — all must hold:

| Metric | Requirement |
|---|---|
| S2 / S3 `stream_p99_ms` | **must improve** toward the 158 ms S1 baseline (from 210 / 217) |
| S2 / S3 `stream_p50_ms` | no worse than baseline (143 / 153) |
| S2 / S3 `import_MB/s` | no worse than baseline (458 / 445) |
| S3 / S4 / S5 `stat/s` | no worse than baseline (1 111 / 26 575 / 1 947) |
| every scenario | `dispatch_timeouts` and `other_errors` remain `0` |

If stream p99 did not improve, **stop and report** — Tasks 2 and 3 did not achieve their purpose and the design needs revisiting before release. If any throughput number regressed, that violates the Global Constraint; stop and report.

- [ ] **Step 3: Re-run the CPU profile and confirm the contention dropped**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
ALTMOUNT_BENCH_DURATION=12s go test ./internal/pool/ -run '^$' \
  -bench 'BenchmarkContention$/import\+health_nostream' -benchtime 1x \
  -cpuprofile /tmp/cpu-after.prof -o /tmp/pool.test -timeout 10m > /dev/null 2>&1
go tool pprof -top -cum -nodecount=400 /tmp/pool.test /tmp/cpu-after.prof 2>/dev/null \
  | grep -E "runtime\.(lock2|selectgo)"
cat /tmp/lock-baseline.txt
```

Expected: `lock2` and `selectgo` shares both below the Task 1 Step 5 baseline (~16.4 % / ~10.7 %).

- [ ] **Step 4: Tag the nntppool release**

```bash
cd /Users/javi/mio/nntppool
make check
git tag v4.19.0
git push origin main --tags
```

- [ ] **Step 5: Drop the replace directive and bump AltMount**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go mod edit -dropreplace github.com/javi11/nntppool/v4
go get github.com/javi11/nntppool/v4@v4.19.0
go mod tidy
grep -n "nntppool" go.mod
```

Expected: `github.com/javi11/nntppool/v4 v4.19.0` and **no** `replace` line.

- [ ] **Step 6: Full verification**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
go build ./... && go vet ./... && go test -race ./internal/pool/ ./internal/testsupport/nntpserver/ -count=1
```

Expected: all pass.

- [ ] **Step 7: Commit**

```bash
cd /Users/javi/orca/workspaces/altmount/oystercatcher
git add go.mod go.sum
git commit -m "chore(deps): bump nntppool to v4.19.0 for priority dispatch fixes"
```

---

## Not in this plan

Spec phases **1d** (AltMount config correctness: `0 = adapt`, knob rename + migration + frontend Auto affordance) and **1e** (re-measure `ImportBudget`) get their own plan, written against the released `v4.19.0`. Spec phase **2** (import body pipelining) is gated on phase 1's numbers and gets a third plan.
