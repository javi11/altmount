package segcache

import (
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
)

func ceilingSource(memMB int) *Source {
	cfg := config.DefaultConfig()
	cfg.SegmentCache.MemoryMB = &memMB
	return NewSource(func() *config.Config { return cfg })
}

func TestSourceCeilingCapsMemoryTierAndReopenKeepsIt(t *testing.T) {
	s := ceilingSource(256)
	s.Store()
	if got := s.Memory().Capacity(); got != int64(256)<<20 {
		t.Fatalf("initial capacity = %d, want 256 MiB", got)
	}
	s.SetMemoryCeiling(64 << 20)
	if got := s.Memory().Capacity(); got != 64<<20 {
		t.Fatalf("capacity after ceiling = %d, want 64 MiB (applied immediately)", got)
	}
	// A file open re-resolves the store; the ceiling must survive it.
	s.Store()
	if got := s.Memory().Capacity(); got != 64<<20 {
		t.Fatalf("capacity after reopen = %d, want ceiling to hold", got)
	}
	s.SetMemoryCeiling(noCeiling)
	if got := s.Memory().Capacity(); got != int64(256)<<20 {
		t.Fatalf("capacity after release = %d, want configured 256 MiB", got)
	}
}

func TestSourceCeilingAboveConfigIsNoop(t *testing.T) {
	s := ceilingSource(64)
	s.SetMemoryCeiling(512 << 20)
	s.Store()
	if got := s.Memory().Capacity(); got != 64<<20 {
		t.Fatalf("capacity = %d, want configured 64 MiB", got)
	}
}

func TestSourceCeilingBeforeFirstOpenApplies(t *testing.T) {
	s := ceilingSource(256)
	s.SetMemoryCeiling(32 << 20)
	s.Store()
	if got := s.Memory().Capacity(); got != 32<<20 {
		t.Fatalf("capacity = %d, want 32 MiB", got)
	}
}
