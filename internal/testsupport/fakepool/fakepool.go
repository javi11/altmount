// Package fakepool provides a deterministic in-process replacement for
// nntppool.Client used by tests that exercise AltMount's streaming and
// connection-management invariants.
//
// The real nntppool.Client opens TCP sockets, dials providers, and runs
// background reconnect / quota / pipelining goroutines. None of that is
// useful when the question under test is "does the streaming pipeline cap
// in-flight downloads", "does a slow provider cause a retry storm", or
// "do ephemeral readers leak goroutines". For those questions we need a
// client we can drive moment-by-moment: control latency, inject specific
// errors, count concurrent calls, and observe whether the caller honors
// the backpressure we apply.
//
// Client satisfies pool.NntpClient (see internal/pool/nntpclient.go). The
// production code never sees a difference; tests inject *Client directly via
// a pool getter closure that returns it as pool.NntpClient.
//
// # Observability primitives
//
// Every Body / BodyPriority / BodyAsync / Stat call increments InFlight on
// entry and decrements on exit. MaxInFlight records the high-water mark.
// Tests assert against these counters to pin invariants like
// "no more than N segment downloads are ever in flight".
//
// # Failure injection
//
// SegmentBehavior controls per-message-ID latency, byte payload, and error.
// Set via SetBehavior or SetDefaultBehavior. Behaviors are evaluated at the
// start of each call; later changes apply only to subsequent calls.
//
// # Backpressure simulation
//
// BlockUntil pins every in-flight call at the entry semaphore until the
// caller closes the returned channel. Use it to hold connections "open"
// while observing how the rest of the pipeline reacts.
//
// All public methods are safe for concurrent use.
package fakepool

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v4"
)

// compile-time assertion: Client must satisfy the narrow interface.
var _ pool.NntpClient = (*Client)(nil)

// SegmentBehavior describes how the fake should respond to a single
// message-ID. The zero value returns an empty body with no delay.
type SegmentBehavior struct {
	// Latency is the wall-clock delay added before the call returns.
	// Honors ctx cancellation: if the context fires first, the call
	// returns ctx.Err() and does not pretend to have produced data.
	Latency time.Duration

	// Bytes is the payload returned in ArticleBody.Bytes. Length is also
	// used for BytesDecoded.
	Bytes []byte

	// Err, if non-nil, is returned instead of a body. Use nntppool sentinel
	// errors (e.g. nntppool.ErrArticleNotFound) to exercise specific paths
	// in the retry/dispatch logic.
	Err error
}

// Client is a fake nntppool.Client suitable for unit and concurrency tests.
type Client struct {
	mu              sync.RWMutex
	defaultBehavior SegmentBehavior
	perSegment      map[string]SegmentBehavior
	releaseGate     <-chan struct{} // nil = no gate; closed = always permit
	stats           nntppool.ClientStats
	hasFixedStats   bool

	// Atomic counters for observability.
	inFlight       atomic.Int32
	maxInFlight    atomic.Int32
	totalCalls     atomic.Int64
	bodyCalls      atomic.Int64
	bodyPriCalls   atomic.Int64
	bodyAsyncCalls atomic.Int64
	statCalls      atomic.Int64

	// Per-message-ID call counts (string → *atomic.Int64). Tests that need
	// to assert how often a specific segment was requested (e.g. to detect
	// retry storms) read this map via PerMessageCalls.
	perIDCalls sync.Map
}

// New returns a fake client. Without further configuration it returns an
// empty (zero-byte) ArticleBody immediately for every message-ID.
func New() *Client {
	return &Client{
		perSegment: make(map[string]SegmentBehavior),
	}
}

// SetDefaultBehavior sets the behavior used for any message-ID that has no
// per-segment override.
func (c *Client) SetDefaultBehavior(b SegmentBehavior) {
	c.mu.Lock()
	c.defaultBehavior = b
	c.mu.Unlock()
}

// SetBehavior sets a per-message-ID behavior. Overrides SetDefaultBehavior.
func (c *Client) SetBehavior(messageID string, b SegmentBehavior) {
	c.mu.Lock()
	c.perSegment[messageID] = b
	c.mu.Unlock()
}

// BlockUntil installs a gate that pins every subsequent call inside the
// fake (after counter increment, before doing any work) until release is
// closed. Useful for asserting "exactly N calls are concurrently in flight
// while the gate is closed".
//
// Pass nil to remove the gate. Setting a new gate replaces any prior one;
// previously gated calls observing the old gate continue to wait on it.
func (c *Client) BlockUntil(release <-chan struct{}) {
	c.mu.Lock()
	c.releaseGate = release
	c.mu.Unlock()
}

// SetStats overrides what Stats() returns. Tests that exercise the metrics
// tracker can use this to feed deterministic provider data.
func (c *Client) SetStats(s nntppool.ClientStats) {
	c.mu.Lock()
	c.stats = s
	c.hasFixedStats = true
	c.mu.Unlock()
}

// InFlight returns the number of calls currently inside the fake.
func (c *Client) InFlight() int32 { return c.inFlight.Load() }

// MaxInFlight returns the high-water mark of InFlight observed since the
// client was created (or since the last ResetCounters).
func (c *Client) MaxInFlight() int32 { return c.maxInFlight.Load() }

// TotalCalls returns the total number of method invocations served.
func (c *Client) TotalCalls() int64 { return c.totalCalls.Load() }

// BodyCalls returns the count of Body invocations.
func (c *Client) BodyCalls() int64 { return c.bodyCalls.Load() }

// BodyPriorityCalls returns the count of BodyPriority invocations. This is
// the most useful counter for streaming-path tests since UsenetReader uses
// BodyPriority exclusively.
func (c *Client) BodyPriorityCalls() int64 { return c.bodyPriCalls.Load() }

// BodyAsyncCalls returns the count of BodyAsync invocations.
func (c *Client) BodyAsyncCalls() int64 { return c.bodyAsyncCalls.Load() }

// StatCalls returns the count of Stat invocations.
func (c *Client) StatCalls() int64 { return c.statCalls.Load() }

// PerMessageCalls returns how many times the given message-ID was requested
// across all method types (Body / BodyPriority / BodyAsync / Stat).
//
// This is the primary signal for retry-storm tests: if a retry policy is
// re-issuing failed requests, the per-ID count climbs faster than the
// number of distinct segments and the assertion fails.
func (c *Client) PerMessageCalls(messageID string) int64 {
	v, ok := c.perIDCalls.Load(messageID)
	if !ok {
		return 0
	}
	return v.(*atomic.Int64).Load()
}

// ResetCounters zeroes all observability counters. Behaviors and gates are
// preserved. Useful between phases of a single test.
func (c *Client) ResetCounters() {
	c.inFlight.Store(0)
	c.maxInFlight.Store(0)
	c.totalCalls.Store(0)
	c.bodyCalls.Store(0)
	c.bodyPriCalls.Store(0)
	c.bodyAsyncCalls.Store(0)
	c.statCalls.Store(0)
	c.perIDCalls.Range(func(k, _ any) bool {
		c.perIDCalls.Delete(k)
		return true
	})
}

// countMessage increments the per-ID counter atomically, lazily creating
// the counter on first contact.
func (c *Client) countMessage(messageID string) {
	if v, ok := c.perIDCalls.Load(messageID); ok {
		v.(*atomic.Int64).Add(1)
		return
	}
	var fresh atomic.Int64
	fresh.Add(1)
	actual, loaded := c.perIDCalls.LoadOrStore(messageID, &fresh)
	if loaded {
		actual.(*atomic.Int64).Add(1)
	}
}

// enter increments in-flight counters and waits at the gate (if any).
// Returns a function to call on exit.
func (c *Client) enter() func() {
	cur := c.inFlight.Add(1)
	c.totalCalls.Add(1)
	for {
		hwm := c.maxInFlight.Load()
		if cur <= hwm || c.maxInFlight.CompareAndSwap(hwm, cur) {
			break
		}
	}
	c.mu.RLock()
	gate := c.releaseGate
	c.mu.RUnlock()
	if gate != nil {
		<-gate
	}
	return func() { c.inFlight.Add(-1) }
}

// behaviorFor resolves the SegmentBehavior for a given message-ID, falling
// back to the default.
func (c *Client) behaviorFor(messageID string) SegmentBehavior {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if b, ok := c.perSegment[messageID]; ok {
		return b
	}
	return c.defaultBehavior
}

// waitOrCancel waits for d to elapse or ctx to fire, whichever first.
// Returns ctx.Err() on cancellation, nil otherwise.
func waitOrCancel(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// Body satisfies pool.NntpClient. Returns either the configured error or an
// ArticleBody filled with the configured Bytes after the configured latency.
func (c *Client) Body(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	c.bodyCalls.Add(1)
	c.countMessage(messageID)
	defer c.enter()()
	return c.serveBody(ctx, messageID, nil)
}

// BodyPriority is identical to Body but counted separately so tests can
// distinguish streaming from importer traffic.
func (c *Client) BodyPriority(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	c.bodyPriCalls.Add(1)
	c.countMessage(messageID)
	defer c.enter()()
	return c.serveBody(ctx, messageID, nil)
}

// BodyAsync streams the configured Bytes (or error) to w and yields a
// BodyResult on the returned channel.
func (c *Client) BodyAsync(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) <-chan nntppool.BodyResult {
	c.bodyAsyncCalls.Add(1)
	c.countMessage(messageID)
	ch := make(chan nntppool.BodyResult, 1)
	go func() {
		defer c.enter()()
		body, err := c.serveBody(ctx, messageID, w)
		ch <- nntppool.BodyResult{Body: body, Err: err}
		close(ch)
	}()
	return ch
}

// Stat returns a StatResult with the message-ID echoed, after the
// configured latency. If the behavior has Err set, it is returned.
func (c *Client) Stat(ctx context.Context, messageID string) (*nntppool.StatResult, error) {
	c.statCalls.Add(1)
	c.countMessage(messageID)
	defer c.enter()()
	b := c.behaviorFor(messageID)
	if err := waitOrCancel(ctx, b.Latency); err != nil {
		return nil, err
	}
	if b.Err != nil {
		return nil, b.Err
	}
	return &nntppool.StatResult{MessageID: messageID}, nil
}

// Stats returns the configured stats or a zero value.
func (c *Client) Stats() nntppool.ClientStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.hasFixedStats {
		return c.stats
	}
	return nntppool.ClientStats{}
}

func (c *Client) serveBody(ctx context.Context, messageID string, w io.Writer) (*nntppool.ArticleBody, error) {
	b := c.behaviorFor(messageID)
	if err := waitOrCancel(ctx, b.Latency); err != nil {
		return nil, err
	}
	if b.Err != nil {
		// Mirror nntppool: on error the result may still carry partial
		// metadata. Tests that need that can extend this; the common case
		// returns nil.
		return nil, b.Err
	}
	payload := b.Bytes
	if w != nil && len(payload) > 0 {
		if _, err := w.Write(payload); err != nil {
			return nil, err
		}
	}
	body := &nntppool.ArticleBody{
		MessageID:    messageID,
		BytesDecoded: len(payload),
	}
	if w == nil {
		body.Bytes = payload
	}
	return body, nil
}

// AssertMaxInFlightLE fails the test if the high-water mark exceeds n.
// Use this as the standard assertion at the end of any test that pins a
// concurrency cap.
func AssertMaxInFlightLE(tb interface {
	Helper()
	Errorf(format string, args ...any)
}, c *Client, n int32) {
	tb.Helper()
	if got := c.MaxInFlight(); got > n {
		tb.Errorf("fakepool: MaxInFlight=%d, want <= %d", got, n)
	}
}

// ErrSimulated502 is a generic transient error suitable for retry-storm
// scenarios. It is wrapped so errors.Is(err, ErrSimulated502) works.
var ErrSimulated502 = errors.New("fakepool: simulated 502 service unavailable")
