package cmd

import (
	"context"
	"log/slog"
	"math"
	"os"
	"runtime/debug"

	"github.com/javi11/altmount/internal/config"
)

// applySoftMemoryLimit sets the configured Go soft memory limit and returns
// it; 0 means the runtime was left as it was.
func applySoftMemoryLimit(ctx context.Context, cfg *config.Config) int64 {
	gomemlimit := os.Getenv("GOMEMLIMIT")
	if gomemlimit != "" {
		return 0
	}
	limit := cfg.SoftMemoryLimit(gomemlimit)
	if limit == 0 {
		debug.SetMemoryLimit(math.MaxInt64)
		return 0
	}
	debug.SetMemoryLimit(limit)
	slog.InfoContext(ctx, "Soft memory limit set",
		"limit_mb", limit>>20, "segment_cache_memory_mb", cfg.SegmentCache.MemoryBytes()>>20,
		"pinned", cfg.MemoryLimitMB != nil && *cfg.MemoryLimitMB > 0)
	return limit
}
