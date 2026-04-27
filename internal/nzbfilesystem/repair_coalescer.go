package nzbfilesystem

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/pkg/rclonecli"
)

// RepairCoalescer throttles per-file streaming-failure repair triggers and
// coalesces rclone VFS refresh calls so that a batch of corrupted reads cannot
// fan out into one RC POST per file.
//
// Two mechanisms:
//
//  1. Per-path debounce — ShouldTrigger returns false if the same file path
//     was triggered within the debounce window. This prevents repeated reads
//     of the same corrupted file from re-firing the repair pipeline.
//
//  2. Directory-level VFS refresh coalescing — EnqueueRefresh adds a virtual
//     directory to a pending set; a single worker goroutine drains the set,
//     deduplicates, and issues at most one RefreshDir RC call at a time with
//     a minimum interval between calls.
type RepairCoalescer struct {
	rclone       rclonecli.RcloneRcClient
	configGetter config.ConfigGetter

	debounceTTL  time.Duration
	flushDelay   time.Duration
	refreshTO    time.Duration

	mu      sync.Mutex
	seen    map[string]time.Time
	pending map[string]struct{}

	wakeCh chan struct{}
	stopCh chan struct{}
	stopWg sync.WaitGroup
}

const (
	defaultRepairDebounceTTL = 30 * time.Second
	defaultRefreshFlushDelay = 1 * time.Second
	defaultRefreshTimeout    = 60 * time.Second
)

// NewRepairCoalescer constructs a coalescer and starts its background worker.
// The worker runs for the lifetime of the process; call Close to stop it in
// tests or during graceful shutdown.
func NewRepairCoalescer(rclone rclonecli.RcloneRcClient, configGetter config.ConfigGetter) *RepairCoalescer {
	c := &RepairCoalescer{
		rclone:       rclone,
		configGetter: configGetter,
		debounceTTL:  defaultRepairDebounceTTL,
		flushDelay:   defaultRefreshFlushDelay,
		refreshTO:    defaultRefreshTimeout,
		seen:         make(map[string]time.Time),
		pending:      make(map[string]struct{}),
		wakeCh:       make(chan struct{}, 1),
		stopCh:       make(chan struct{}),
	}
	c.stopWg.Add(1)
	go c.run()
	return c
}

// ShouldTrigger reports whether a streaming-failure repair should be fired for
// path. It returns true at most once per debounce window per path. Stale
// entries are pruned opportunistically on each call.
func (c *RepairCoalescer) ShouldTrigger(path string) bool {
	if c == nil {
		return true
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()

	if last, ok := c.seen[path]; ok && now.Sub(last) < c.debounceTTL {
		return false
	}
	c.seen[path] = now

	// Opportunistic prune: cap the map growth on long-running processes.
	if len(c.seen) > 1024 {
		cutoff := now.Add(-c.debounceTTL)
		for k, t := range c.seen {
			if t.Before(cutoff) {
				delete(c.seen, k)
			}
		}
	}
	return true
}

// EnqueueRefresh adds virtualDir to the set of directories that will be sent
// to rclone in the next batched vfs/refresh call. Duplicate directories within
// the same flush window collapse to a single entry.
func (c *RepairCoalescer) EnqueueRefresh(virtualDir string) {
	if c == nil || c.rclone == nil || virtualDir == "" {
		return
	}
	c.mu.Lock()
	c.pending[virtualDir] = struct{}{}
	c.mu.Unlock()

	select {
	case c.wakeCh <- struct{}{}:
	default:
	}
}

// Close stops the background worker. Pending refreshes are dropped.
func (c *RepairCoalescer) Close() {
	if c == nil {
		return
	}
	select {
	case <-c.stopCh:
		return
	default:
	}
	close(c.stopCh)
	c.stopWg.Wait()
}

func (c *RepairCoalescer) run() {
	defer c.stopWg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		case <-c.wakeCh:
		}

		// Coalescing window: let a burst accumulate before flushing so that
		// 12 concurrent streaming failures collapse into one RC call.
		select {
		case <-c.stopCh:
			return
		case <-time.After(c.flushDelay):
		}

		c.flush()
	}
}

func (c *RepairCoalescer) flush() {
	c.mu.Lock()
	if len(c.pending) == 0 {
		c.mu.Unlock()
		return
	}
	dirs := make([]string, 0, len(c.pending))
	for d := range c.pending {
		dirs = append(dirs, d)
	}
	c.pending = make(map[string]struct{})
	c.mu.Unlock()

	cfg := c.configGetter()
	vfsName := cfg.RClone.VFSName
	if vfsName == "" {
		vfsName = config.MountProvider
	}

	ctx, cancel := context.WithTimeout(context.Background(), c.refreshTO)
	defer cancel()

	if err := c.rclone.RefreshDir(ctx, vfsName, dirs); err != nil {
		slog.ErrorContext(ctx, "Failed to notify rclone VFS about file status changes after streaming failure",
			"dir_count", len(dirs),
			"err", err)
		return
	}
	slog.DebugContext(ctx, "Coalesced rclone VFS refresh dispatched",
		"dir_count", len(dirs))
}
