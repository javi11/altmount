package segcache

import (
	"sync"
	"sync/atomic"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/usenet"
)

// Source resolves the SegmentStore readers use: a memory tier that is on by
// default, in front of the optional disk cache owned by Manager. It is the
// single value threaded through the application instead of passing raw
// atomic pointers and getter closures.
type Source struct {
	ptr    atomic.Pointer[Manager]
	getCfg config.ConfigGetter

	once   sync.Once
	tiered *TieredStore
}

// NewSource creates a Source. getCfg must not be nil.
func NewSource(getCfg config.ConfigGetter) *Source {
	return &Source{getCfg: getCfg}
}

// Store resolves the current SegmentStore: nil only when both the memory
// and the disk tier are disabled. Call once at file-open time and pass the
// result to UsenetReader. Capacity changes in config apply on the next open.
func (s *Source) Store() usenet.SegmentStore {
	cfg := s.getCfg()
	memBytes := int64(cfg.SegmentCache.MemoryBytes())

	var disk usenet.SegmentStore
	if mgr := s.ptr.Load(); mgr != nil && cfg.SegmentCache.Enabled != nil && *cfg.SegmentCache.Enabled {
		disk = mgr.Cache()
	}
	if memBytes <= 0 && disk == nil {
		return nil
	}
	s.once.Do(func() { s.tiered = NewTieredStore(NewMemoryCache(memBytes)) })
	s.tiered.Memory().SetCapacity(memBytes)
	s.tiered.SetDisk(disk)
	return s.tiered
}

// Swap replaces the active manager. Pass nil to unload the current manager.
// The caller is responsible for stopping the old manager before calling Swap.
func (s *Source) Swap(mgr *Manager) {
	s.ptr.Store(mgr)
}

// Manager returns the current manager for stats access. May be nil.
func (s *Source) Manager() *Manager {
	return s.ptr.Load()
}

// Memory returns the memory tier for stats, or nil before the first open.
func (s *Source) Memory() *MemoryCache {
	if s.tiered == nil {
		return nil
	}
	return s.tiered.Memory()
}
