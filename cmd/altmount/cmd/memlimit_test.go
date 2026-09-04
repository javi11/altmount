package cmd

import (
	"context"
	"math"
	"runtime/debug"
	"testing"

	"github.com/javi11/altmount/internal/config"
)

func TestApplySoftMemoryLimitSetsRuntimeLimit(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	t.Setenv("GOMEMLIMIT", "")

	mb := 128
	cfg := &config.Config{SegmentCache: config.SegmentCacheConfig{MemoryMB: &mb}}
	got := applySoftMemoryLimit(context.Background(), cfg)

	want := cfg.SegmentCache.SoftMemoryLimit("")
	if got != want || debug.SetMemoryLimit(-1) != want {
		t.Fatalf("applied %d, runtime %d, want %d", got, debug.SetMemoryLimit(-1), want)
	}
}

func TestApplySoftMemoryLimitLeavesRuntimeWhenDisabled(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	t.Setenv("GOMEMLIMIT", "1GiB")
	operator := int64(1) << 30
	debug.SetMemoryLimit(operator)

	mb := 128
	cfg := &config.Config{SegmentCache: config.SegmentCacheConfig{MemoryMB: &mb}}
	if got := applySoftMemoryLimit(context.Background(), cfg); got != 0 {
		t.Fatalf("applied %d, want 0", got)
	}
	if debug.SetMemoryLimit(-1) != operator {
		t.Fatalf("runtime limit changed to %d", debug.SetMemoryLimit(-1))
	}
}

func TestApplySoftMemoryLimitClearsLimitWhenMemoryTierOff(t *testing.T) {
	prev := debug.SetMemoryLimit(-1)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	t.Setenv("GOMEMLIMIT", "")
	debug.SetMemoryLimit(512 << 20)

	zero := 0
	cfg := &config.Config{SegmentCache: config.SegmentCacheConfig{MemoryMB: &zero}}
	if got := applySoftMemoryLimit(context.Background(), cfg); got != 0 {
		t.Fatalf("applied %d, want 0", got)
	}
	if debug.SetMemoryLimit(-1) != math.MaxInt64 {
		t.Fatalf("runtime limit %d, want cleared", debug.SetMemoryLimit(-1))
	}
}
