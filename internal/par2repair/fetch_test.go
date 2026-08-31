package par2repair

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
)

// fakeStatClient satisfies BodyClient plus the stat surface PoolFetcher
// probes for; liveness is answered from the articles set.
type fakeStatClient struct {
	articles map[string]bool
}

func (c *fakeStatClient) Body(_ context.Context, messageID string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	if !c.articles[messageID] {
		return nil, nntppool.ErrArticleNotFound
	}
	return &nntppool.ArticleBody{}, nil
}

func (c *fakeStatClient) StatMany(_ context.Context, messageIDs []string, _ nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	out := make(chan nntppool.StatManyResult, len(messageIDs))
	go func() {
		defer close(out)
		for _, id := range messageIDs {
			if c.articles[id] {
				out <- nntppool.StatManyResult{MessageID: id, Result: &nntppool.StatResult{MessageID: id}}
			} else {
				out <- nntppool.StatManyResult{MessageID: id, Err: nntppool.ErrArticleNotFound}
			}
		}
	}()
	return out
}

func TestPoolFetcherStatIDs(t *testing.T) {
	client := &fakeStatClient{articles: map[string]bool{"live@x": true}}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)

	missing, err := f.StatIDs(context.Background(), []string{"live@x", "gone@x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing["live@x"] || !missing["gone@x"] {
		t.Fatalf("missing = %v", missing)
	}
}

// flakyStatClient answers StatMany with a transient error for the first
// failures[id] calls per id, then with the article's real verdict.
type flakyStatClient struct {
	articles map[string]bool
	mu       sync.Mutex
	failures map[string]int
	calls    map[string]int
}

func (c *flakyStatClient) Body(_ context.Context, messageID string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	if !c.articles[messageID] {
		return nil, nntppool.ErrArticleNotFound
	}
	return &nntppool.ArticleBody{}, nil
}

func (c *flakyStatClient) StatMany(_ context.Context, messageIDs []string, _ nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	out := make(chan nntppool.StatManyResult, len(messageIDs))
	go func() {
		defer close(out)
		for _, id := range messageIDs {
			c.mu.Lock()
			if c.calls == nil {
				c.calls = map[string]int{}
			}
			c.calls[id]++
			transient := c.failures[id] > 0
			if transient {
				c.failures[id]--
			}
			c.mu.Unlock()
			switch {
			case transient:
				out <- nntppool.StatManyResult{MessageID: id, Err: errors.New("nntp: all providers exhausted: connection died")}
			case c.articles[id]:
				out <- nntppool.StatManyResult{MessageID: id, Result: &nntppool.StatResult{MessageID: id}}
			default:
				out <- nntppool.StatManyResult{MessageID: id, Err: nntppool.ErrArticleNotFound}
			}
		}
	}()
	return out
}

// A STAT that fails with a transport error proves nothing about the article.
// Counting it alive poisons planning (a flapping provider makes a mostly-dead
// release look healthy), so unresolved ids must be re-STATed until they yield
// a real verdict.
func TestPoolFetcherStatIDsRetriesUnresolved(t *testing.T) {
	client := &flakyStatClient{
		articles: map[string]bool{"live@x": true},
		failures: map[string]int{"gone@x": 2, "live@x": 1},
	}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond

	missing, err := f.StatIDs(context.Background(), []string{"live@x", "gone@x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing["live@x"] || !missing["gone@x"] {
		t.Fatalf("missing = %v, want only gone@x after retrying transient errors", missing)
	}
	if got := client.calls["gone@x"]; got != 3 {
		t.Fatalf("StatMany calls for gone@x = %d, want 3", got)
	}
}

// A handful of still-unresolved articles after retries is absorbable — the
// payload sweep verifies every slice anyway — so the sweep result stands,
// with the unresolved ids simply not reported missing.
func TestPoolFetcherStatIDsToleratesFewUnresolved(t *testing.T) {
	client := &flakyStatClient{
		articles: map[string]bool{"live@x": true},
		failures: map[string]int{"stuck@x": 99},
	}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond

	missing, err := f.StatIDs(context.Background(), []string{"live@x", "stuck@x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing = %v, want empty (unresolved is not a verdict)", missing)
	}
}

// When transient failures dominate the sweep even after retries, the liveness
// picture is fiction: reporting it would let planning proceed on false "alive"
// verdicts (observed live: a flapping provider made a ~90%-dead release sweep
// clean, costing 10 minutes of serial dead-article discovery). The sweep must
// fail so the attempt is retried later instead.
func TestPoolFetcherStatIDsFailsWhenUnresolvedDominates(t *testing.T) {
	n := maxHiddenAbsorbArticles + 1
	ids := make([]string, n)
	failures := map[string]int{}
	for i := range ids {
		ids[i] = fmt.Sprintf("stuck%d@x", i)
		failures[ids[i]] = 99
	}
	client := &flakyStatClient{failures: failures}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond

	_, err := f.StatIDs(context.Background(), ids, nil)
	if err == nil {
		t.Fatal("StatIDs must fail when most of the sweep stays unresolved")
	}
	if errors.Is(err, ErrUnrepairable) {
		t.Fatalf("err = %v, must be transient, not unrepairable", err)
	}
}

// concurrencyRecordingStatClient records the StatManyOptions each pass used.
type concurrencyRecordingStatClient struct {
	fakeStatClient
	mu   sync.Mutex
	opts []nntppool.StatManyOptions
}

func (c *concurrencyRecordingStatClient) StatMany(ctx context.Context, messageIDs []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	c.mu.Lock()
	c.opts = append(c.opts, opts)
	c.mu.Unlock()
	return c.fakeStatClient.StatMany(ctx, messageIDs, opts)
}

// An unbounded sweep lets the pool derive concurrency from its aggregate STAT
// pipeline capacity — thousands of simultaneous STATs that saturate every
// provider's dispatch window and fail the sweep itself with "all providers
// exhausted" (observed live). The sweep must cap its own burst.
func TestPoolFetcherStatIDsBoundsConcurrency(t *testing.T) {
	client := &concurrencyRecordingStatClient{fakeStatClient: fakeStatClient{articles: map[string]bool{"live@x": true}}}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)

	if _, err := f.StatIDs(context.Background(), []string{"live@x", "gone@x"}, nil); err != nil {
		t.Fatal(err)
	}
	if len(client.opts) == 0 {
		t.Fatal("StatMany never called")
	}
	for _, o := range client.opts {
		if o.Concurrency <= 0 || o.Concurrency > statSweepConcurrency {
			t.Fatalf("Concurrency = %d, want in (0, %d]", o.Concurrency, statSweepConcurrency)
		}
	}
}

// bodyOnlyClient has no stat surface; StatIDs must degrade to a no-op.
type bodyOnlyClient struct{}

func (bodyOnlyClient) Body(_ context.Context, _ string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	return &nntppool.ArticleBody{}, nil
}

func TestPoolFetcherStatIDsWithoutStatSupport(t *testing.T) {
	f := NewPoolFetcher(func() (BodyClient, error) { return bodyOnlyClient{}, nil }, nil)

	missing, err := f.StatIDs(context.Background(), []string{"a@x"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("missing = %v, want nil (capability absent)", missing)
	}
}

// flakyBodyClient fails the first n Body calls per message with a transient
// pool error, then succeeds.
type flakyBodyClient struct {
	mu       sync.Mutex
	failures map[string]int // remaining transient failures per id
	calls    map[string]int
	err      error
}

func (c *flakyBodyClient) Body(_ context.Context, messageID string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.calls == nil {
		c.calls = map[string]int{}
	}
	c.calls[messageID]++
	if c.failures[messageID] > 0 {
		c.failures[messageID]--
		return nil, c.err
	}
	return &nntppool.ArticleBody{Bytes: []byte("payload")}, nil
}

// A single transient pool failure ("all providers exhausted" during a network
// blip) must not surface: one failed article fetch aborts a repair attempt
// that took 20 minutes of sweeping, so the fetcher retries transient errors
// with a short backoff before giving up.
func TestPoolFetcherRetriesTransientErrors(t *testing.T) {
	client := &flakyBodyClient{
		failures: map[string]int{"blip@x": 2},
		err:      errors.New("nntp: all providers exhausted"),
	}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond // keep the test fast

	data, err := f.Fetch(context.Background(), "blip@x")
	if err != nil {
		t.Fatalf("Fetch must absorb transient pool errors, got: %v", err)
	}
	if string(data) != "payload" {
		t.Fatalf("payload = %q", data)
	}
	if got := client.calls["blip@x"]; got != 3 {
		t.Fatalf("Body calls = %d, want 3 (two transient failures then success)", got)
	}
}

// A 430 is a definitive verdict, not a blip: retrying it would slow every
// dead-article discovery by the whole backoff ladder.
func TestPoolFetcherDoesNotRetryArticleNotFound(t *testing.T) {
	client := &flakyBodyClient{
		failures: map[string]int{"gone@x": 99},
		err:      nntppool.ErrArticleNotFound,
	}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond

	if _, err := f.Fetch(context.Background(), "gone@x"); !errors.Is(err, nntppool.ErrArticleNotFound) {
		t.Fatalf("err = %v, want ErrArticleNotFound", err)
	}
	if got := client.calls["gone@x"]; got != 1 {
		t.Fatalf("Body calls = %d, want 1 (no retry on a definitive 430)", got)
	}
}

// Persistent failure still surfaces after the retry budget, and a cancelled
// context aborts the backoff wait immediately.
func TestPoolFetcherRetryBudgetAndCancellation(t *testing.T) {
	client := &flakyBodyClient{
		failures: map[string]int{"down@x": 99},
		err:      errors.New("nntp: all providers exhausted"),
	}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond

	if _, err := f.Fetch(context.Background(), "down@x"); err == nil {
		t.Fatal("persistent failure must surface after the retry budget")
	}
	budgetCalls := client.calls["down@x"]
	if budgetCalls < 2 {
		t.Fatalf("Body calls = %d, want the whole retry budget spent", budgetCalls)
	}

	f.retryDelay = time.Hour // cancellation must not wait this out
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := f.Fetch(ctx, "down@x")
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled fetch must fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled fetch must not sit in the retry backoff")
	}
}

// The repair's connection limiter must bound concurrent acquisitions to the
// live config value — including picking up a raised limit mid-flight, so a
// config change speeds up the next fetches without a restart.
func TestConnLimiterBoundsConcurrency(t *testing.T) {
	limit := 2
	var mu sync.Mutex
	limiter := NewConnLimiter(func() int {
		mu.Lock()
		defer mu.Unlock()
		return limit
	})

	var cur, peak int32
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := limiter.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
			release()
		}()
	}
	wg.Wait()
	if p := atomic.LoadInt32(&peak); p > 2 {
		t.Fatalf("peak concurrency = %d, want <= 2", p)
	}

	// Raising the limit lets more through.
	mu.Lock()
	limit = 4
	mu.Unlock()
	atomic.StoreInt32(&peak, 0)
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			release, err := limiter.Acquire(context.Background())
			if err != nil {
				t.Error(err)
				return
			}
			n := atomic.AddInt32(&cur, 1)
			for {
				p := atomic.LoadInt32(&peak)
				if n <= p || atomic.CompareAndSwapInt32(&peak, p, n) {
					break
				}
			}
			time.Sleep(20 * time.Millisecond)
			atomic.AddInt32(&cur, -1)
			release()
		}()
	}
	wg.Wait()
	if p := atomic.LoadInt32(&peak); p <= 2 || p > 4 {
		t.Fatalf("peak concurrency after raise = %d, want in (2, 4]", p)
	}

	// A cancelled context aborts a blocked acquire.
	blockers := make([]func(), 0, 4)
	for range 4 {
		release, err := limiter.Acquire(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		blockers = append(blockers, release)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := limiter.Acquire(ctx)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("acquire on a cancelled context must fail")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancelled acquire did not unblock")
	}
	for _, release := range blockers {
		release()
	}
}

// wedgedBodyClient simulates the observed nntppool failure: a Body call whose
// response is never delivered (committed request orphaned by a dying
// connection). It returns only when the caller's context expires.
type wedgedBodyClient struct {
	mu    sync.Mutex
	calls int
}

func (c *wedgedBodyClient) Body(ctx context.Context, _ string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	c.mu.Lock()
	n := c.calls
	c.calls++
	c.mu.Unlock()
	if n == 0 {
		<-ctx.Done() // wedged until the per-attempt deadline fires
		return nil, ctx.Err()
	}
	return &nntppool.ArticleBody{Bytes: []byte("payload")}, nil
}

// One wedged Body call must not freeze a repair until the job timeout: each
// attempt carries its own deadline, after which the fetch retries on a fresh
// request. (Observed live: committed requests orphaned by dying connections
// waited forever while dozens of slots idled.)
func TestPoolFetcherAttemptTimeoutUnwedgesBody(t *testing.T) {
	client := &wedgedBodyClient{}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond
	f.attemptTimeout = 50 * time.Millisecond

	start := time.Now()
	data, err := f.Fetch(context.Background(), "wedged@x")
	if err != nil {
		t.Fatalf("Fetch = %v, want retry success after the wedged attempt", err)
	}
	if string(data) != "payload" {
		t.Fatalf("payload = %q", data)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Fetch took %v, want the attempt timeout to cut the wedged call short", elapsed)
	}
	if client.calls != 2 {
		t.Fatalf("Body calls = %d, want 2 (wedged attempt + retry)", client.calls)
	}
}

// The attempt deadline must not mask the caller's own cancellation: a
// cancelled parent context surfaces as such, without retries.
func TestPoolFetcherAttemptTimeoutPreservesCallerCancel(t *testing.T) {
	client := &wedgedBodyClient{}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)
	f.retryDelay = time.Millisecond
	f.attemptTimeout = time.Hour

	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	_, err := f.Fetch(ctx, "wedged@x")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if client.calls != 1 {
		t.Fatalf("Body calls = %d, want 1 (no retry after caller cancel)", client.calls)
	}
}
