package config

import (
	"log/slog"
	"os"
	"sync"
)

// DefaultLoggingUpdater manages dynamic logging level updates
type DefaultLoggingUpdater struct {
	currentDebug bool
	mutex        sync.RWMutex
}

// NewLoggingUpdater creates a new logging updater
func NewLoggingUpdater(initialDebug bool) LoggingUpdater {
	return &DefaultLoggingUpdater{
		currentDebug: initialDebug,
	}
}

// UpdateDebugMode updates the global logging level based on debug mode
func (u *DefaultLoggingUpdater) UpdateDebugMode(debug bool) error {
	u.mutex.Lock()
	defer u.mutex.Unlock()
	
	if u.currentDebug == debug {
		return nil // No change needed
	}
	
	u.currentDebug = debug
	
	// Create new logger with appropriate level
	var level slog.Level
	if debug {
		level = slog.LevelDebug
	} else {
		level = slog.LevelInfo
	}
	
	// Create new structured logger
	opts := &slog.HandlerOptions{
		Level: level,
	}
	
	handler := slog.NewTextHandler(os.Stdout, opts)
	logger := slog.New(handler)
	
	// Set as default logger
	slog.SetDefault(logger)
	
	return nil
}

// GetDebugMode returns the current debug mode status
func (u *DefaultLoggingUpdater) GetDebugMode() bool {
	u.mutex.RLock()
	defer u.mutex.RUnlock()
	return u.currentDebug
}