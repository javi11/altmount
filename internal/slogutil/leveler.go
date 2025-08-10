package slogutil

import (
	"log/slog"
	"sync/atomic"
)

type DynamicLeveler struct {
	level atomic.Value
}

// Level returns the current logging level.
func (dl *DynamicLeveler) Level() slog.Level {
	return dl.level.Load().(slog.Level)
}

// SetLevel updates the logging level.
func (dl *DynamicLeveler) SetLevel(level slog.Level) {
	dl.level.Store(level)
}
