package par2repair

import (
	"context"
	"errors"
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
