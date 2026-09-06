package pool

import "sync"

// SpeculativeBudget bounds read-ahead fetches across every stream on the
// pool. Without it each open handle schedules its own full prefetch window,
// so several players on one title flood the priority lane with speculative
// bodies and starve each other's demand reads. Demand fetches never take a
// slot; only fetches ahead of the read position do, and only when one is
// free right now.
type SpeculativeBudget struct {
	mu       sync.Mutex
	inFlight int
	capacity int
}

// speculativeBodiesPerConn is how many speculative bodies a connection is
// worth: one below the default per-connection stream cap, so read-ahead can
// fill every connection's pipeline while each still has a slot left for a
// demand read. Providers that serve articles slowly are depth-bound, so a
// smaller figure costs throughput directly: three per connection measured
// about 15% below the full pipeline at a 15-connection budget.
const speculativeBodiesPerConn = 4

func NewSpeculativeBudget() *SpeculativeBudget { return &SpeculativeBudget{} }

// SetCapacity derives the slot count from total provider connections: four
// bodies per connection, minus one so a demand read never finds every slot
// taken by read-ahead. Zero or negative disables the budget (unlimited).
func (b *SpeculativeBudget) SetCapacity(totalConns int) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if totalConns <= 0 {
		b.capacity = 0
		return
	}
	b.capacity = max(totalConns*speculativeBodiesPerConn-1, 1)
}

// Capacity returns the current slot count; 0 means unlimited.
func (b *SpeculativeBudget) Capacity() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.capacity
}

// InFlight returns how many speculative fetches currently hold a slot.
func (b *SpeculativeBudget) InFlight() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.inFlight
}

// TryAcquire never blocks. ok=false means the caller should skip this fetch
// for now and try again when its reader advances. release must be called
// exactly once when ok is true; extra calls are ignored.
func (b *SpeculativeBudget) TryAcquire() (release func(), ok bool) {
	if b == nil {
		return func() {}, true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.capacity == 0 {
		return func() {}, true
	}
	if b.inFlight >= b.capacity {
		return nil, false
	}
	b.inFlight++
	var once sync.Once
	return func() {
		once.Do(func() {
			b.mu.Lock()
			b.inFlight--
			b.mu.Unlock()
		})
	}, true
}
