package segcache

import (
	"sync"
	"sync/atomic"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/usenet"
)

// Source resolves the SegmentStore readers use: a memory tier that is on by
// default, in front of the optional disk cache owned by Manager. It is the
// single value threaded through the application instead of passing raw
// atomic pointers and getter closures.
type Source struct {
	ptr    atomic.Pointer[Manager]
	getCfg config.ConfigGetter

	once   sync.Once
	tiered atomic.Pointer[TieredStore]

	// ceiling caps the memory tier below its configured capacity while the
	// pressure governor sees the heap pressing on the soft memory limit;
	// noCeiling applies the configured value.
	ceiling atomic.Int64
}

// NewSource creates a Source. getCfg must not be nil.
func NewSource(getCfg config.ConfigGetter) *Source {
	s := &Source{getCfg: getCfg}
	s.ceiling.Store(noCeiling)
	return s
}

// SetMemoryCeiling caps the memory tier at bytes (noCeiling removes the cap),
// evicting immediately when the live tier is larger. The cap survives file
// opens, which otherwise re-apply the configured capacity.
func (s *Source) SetMemoryCeiling(bytes int64) {
	if bytes < 0 {
		bytes = noCeiling
	}
	s.ceiling.Store(bytes)
	if t := s.tiered.Load(); t != nil {
		t.Memory().SetCapacity(s.effectiveMemoryBytes(s.getCfg().SegmentCache.MemoryBytes()))
	}
}

// MemoryCeiling is the current cap, or noCeiling.
func (s *Source) MemoryCeiling() int64 { return s.ceiling.Load() }

func (s *Source) effectiveMemoryBytes(configured int64) int64 {
	if c := s.ceiling.Load(); c != noCeiling && c < configured {
		return c
	}
	return configured
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
	memBytes = s.effectiveMemoryBytes(memBytes)
	s.once.Do(func() { s.tiered.Store(NewTieredStore(NewMemoryCache(memBytes))) })
	t := s.tiered.Load()
	t.Memory().SetCapacity(memBytes)
	t.SetDisk(disk)
	return t
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
	t := s.tiered.Load()
	if t == nil {
		return nil
	}
	return t.Memory()
}
