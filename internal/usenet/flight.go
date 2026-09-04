package usenet

import (
	"context"
	"errors"
	"hash/fnv"
	"sync"
)

// articleBuf is one article's decoded bytes as they arrive. buf holds what
// the current attempt has decoded; ready is how much of it readers may see.
// A later attempt starts a fresh buf and swaps it in only once it has passed
// ready, so a reader that already consumed the prefix never sees it move.
// Shared through the flight map, one buffer serves every reader that wants
// the same article at the same time.
type articleBuf struct {
	mu      sync.Mutex
	buf     []byte
	ready   int64
	done    bool
	err     error
	attempt int
	leading bool
	refs    int
	size    int64
	notify  chan struct{} // closed and replaced on every state change
}

// errNoLeader tells a waiting follower that nobody is fetching the article
// any more and it should claim the lead itself.
var errNoLeader = errors.New("usenet: article has no leader")

func newArticleBuf(size int64) *articleBuf {
	return &articleBuf{size: size, notify: make(chan struct{})}
}

// wakeLocked releases every waiter parked on notify. Caller holds mu.
func (a *articleBuf) wakeLocked() {
	close(a.notify)
	a.notify = make(chan struct{})
}

// articleWriter is one fetch attempt's sink.
type articleWriter struct {
	a       *articleBuf
	attempt int
	buf     []byte
}

// attemptWriter starts a new fetch attempt. Only the newest attempt can
// publish, and only past what earlier attempts already made visible.
func (a *articleBuf) attemptWriter() *articleWriter {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.attempt++
	return &articleWriter{a: a, attempt: a.attempt, buf: make([]byte, 0, max(a.size, 0))}
}

func (w *articleWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	w.a.publish(w)
	return len(p), nil
}

// bytes returns everything this attempt received.
func (w *articleWriter) bytes() []byte { return w.buf }

func (a *articleBuf) publish(w *articleWriter) {
	a.mu.Lock()
	if a.done || w.attempt != a.attempt || int64(len(w.buf)) <= a.ready {
		a.mu.Unlock()
		return
	}
	a.buf = w.buf
	a.ready = int64(len(w.buf))
	a.wakeLocked()
	a.mu.Unlock()
}

// finish marks the article complete with the bytes w received.
func (a *articleBuf) finish(w *articleWriter) {
	a.mu.Lock()
	if a.done || w.attempt != a.attempt {
		a.mu.Unlock()
		return
	}
	a.buf = w.buf
	a.ready = int64(len(w.buf))
	a.done = true
	a.leading = false
	a.wakeLocked()
	a.mu.Unlock()
}

// setData completes the article with a whole payload.
func (a *articleBuf) setData(data []byte) {
	a.mu.Lock()
	if a.done {
		a.mu.Unlock()
		return
	}
	a.buf = data
	a.ready = int64(len(data))
	a.done = true
	a.leading = false
	a.wakeLocked()
	a.mu.Unlock()
}

// setError ends the article with an error. Readers already handed a prefix
// see the error once they reach the watermark.
func (a *articleBuf) setError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	if a.err == nil {
		a.err = err
	}
	a.leading = false
	a.wakeLocked()
	a.mu.Unlock()
}

func (a *articleBuf) published() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.ready
}

// claimLead returns true for exactly one caller while the article has no
// active leader and is not finished.
func (a *articleBuf) claimLead() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.leading || a.done || a.err != nil {
		return false
	}
	a.leading = true
	return true
}

// releaseLead lets a follower take over after a leader gave up without
// finishing (its reader was closed mid-article).
func (a *articleBuf) releaseLead() {
	a.mu.Lock()
	a.leading = false
	a.wakeLocked()
	a.mu.Unlock()
}

// waitDone blocks until the article is complete (nil), failed (its error),
// leaderless (errNoLeader), or ctx ends.
func (a *articleBuf) waitDone(ctx context.Context) error {
	for {
		a.mu.Lock()
		switch {
		case a.err != nil:
			err := a.err
			a.mu.Unlock()
			return err
		case a.done:
			a.mu.Unlock()
			return nil
		case !a.leading:
			a.mu.Unlock()
			return errNoLeader
		}
		wait := a.notify
		a.mu.Unlock()
		select {
		case <-wait:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// flightMap holds the articles currently wanted by at least one segment, so
// two readers asking for the same message-ID share one download.
type flightMap struct {
	shards [16]flightShard
}

type flightShard struct {
	mu sync.Mutex
	m  map[string]*articleBuf
}

// flights is the process-wide map; tests inject their own so parallel tests
// reusing message-IDs against different fakes do not join each other.
var flights = newFlightMap()

func newFlightMap() *flightMap { return &flightMap{} }

func (f *flightMap) shard(id string) *flightShard {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return &f.shards[h.Sum32()%uint32(len(f.shards))]
}

// acquire returns the article's shared buffer, creating it on first use,
// and adds a reference.
func (f *flightMap) acquire(id string, size int64) *articleBuf {
	s := f.shard(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]*articleBuf)
	}
	a, ok := s.m[id]
	if !ok {
		a = newArticleBuf(size)
		s.m[id] = a
	}
	a.mu.Lock()
	a.refs++
	a.mu.Unlock()
	return a
}

// release drops a reference and forgets the article once nobody holds it.
func (f *flightMap) release(id string, a *articleBuf) {
	s := f.shard(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	a.mu.Lock()
	a.refs--
	gone := a.refs <= 0
	a.mu.Unlock()
	if gone && s.m[id] == a {
		delete(s.m, id)
	}
}

// len reports how many articles are currently tracked (tests).
func (f *flightMap) len() int {
	n := 0
	for i := range f.shards {
		f.shards[i].mu.Lock()
		n += len(f.shards[i].m)
		f.shards[i].mu.Unlock()
	}
	return n
}
