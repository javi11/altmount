package segcache

import (
	"sync/atomic"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/usenet"
)

// Source is a thin wrapper around an atomic Manager pointer that resolves the
// active SegmentStore on demand. It is the single value threaded through the
// application instead of passing raw atomic pointers and getter closures.
type Source struct {
	ptr    atomic.Pointer[Manager]
	getCfg config.ConfigGetter
}

// NewSource creates a Source. getCfg must not be nil.
func NewSource(getCfg config.ConfigGetter) *Source {
	return &Source{getCfg: getCfg}
}

// Store resolves the current SegmentStore. Returns nil if the cache is
// disabled in config or no manager has been loaded yet.
// Call once at file-open time and pass the result to UsenetReader.
func (s *Source) Store() usenet.SegmentStore {
	mgr := s.ptr.Load()
	if mgr == nil {
		return nil
	}
	cfg := s.getCfg()
	if cfg.SegmentCache.Enabled == nil || !*cfg.SegmentCache.Enabled {
		return nil
	}
	return mgr.Cache()
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
