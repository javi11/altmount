package config

import "log/slog"

// ComponentUpdater defines interface for components that can update their configuration dynamically
type ComponentUpdater interface {
	UpdateConfig(newConfig *Config) error
}

// WorkerPoolUpdater defines interface for components that can resize worker pools
type WorkerPoolUpdater interface {
	UpdateDownloadWorkers(count int) error
	UpdateImportWorkers(count int) error
}

// AuthUpdater defines interface for components that can update authentication
type AuthUpdater interface {
	UpdateAuth(username, password string) error
}

// LoggingUpdater defines interface for components that can update logging levels
type LoggingUpdater interface {
	UpdateDebugMode(debug bool) error
}

// DirectoryUpdater defines interface for components that can update directory paths
type DirectoryUpdater interface {
	UpdateWatchPathectory(path string) error
}

// RCloneUpdater defines interface for components that can update RClone settings
type RCloneUpdater interface {
	UpdateRCloneSettings(password, salt string) error
}

// MetadataUpdater defines interface for components that can update metadata settings
type MetadataUpdater interface {
	UpdateMaxRangeSize(size int64) error
	UpdateStreamingChunkSize(size int64) error
	UpdateMaxDownloadWorkers(count int) error
}

// ComponentRegistry holds references to updatable components
type ComponentRegistry struct {
	WorkerPool WorkerPoolUpdater
	WebDAV     AuthUpdater
	API        AuthUpdater
	Logging    LoggingUpdater
	Directory  DirectoryUpdater
	RClone     RCloneUpdater
	Metadata   MetadataUpdater
	logger     *slog.Logger
}

// NewComponentRegistry creates a new component registry
func NewComponentRegistry(logger *slog.Logger) *ComponentRegistry {
	if logger == nil {
		logger = slog.Default()
	}

	return &ComponentRegistry{
		logger: logger,
	}
}

// RegisterWorkerPool registers a worker pool updater
func (r *ComponentRegistry) RegisterWorkerPool(updater WorkerPoolUpdater) {
	r.WorkerPool = updater
}

// RegisterWebDAV registers a WebDAV auth updater
func (r *ComponentRegistry) RegisterWebDAV(updater AuthUpdater) {
	r.WebDAV = updater
}

// RegisterAPI registers an API auth updater
func (r *ComponentRegistry) RegisterAPI(updater AuthUpdater) {
	r.API = updater
}

// RegisterLogging registers a logging updater
func (r *ComponentRegistry) RegisterLogging(updater LoggingUpdater) {
	r.Logging = updater
}

// RegisterDirectory registers a directory updater
func (r *ComponentRegistry) RegisterDirectory(updater DirectoryUpdater) {
	r.Directory = updater
}

// RegisterRClone registers an RClone updater
func (r *ComponentRegistry) RegisterRClone(updater RCloneUpdater) {
	r.RClone = updater
}

// RegisterMetadata registers a metadata updater
func (r *ComponentRegistry) RegisterMetadata(updater MetadataUpdater) {
	r.Metadata = updater
}

// ApplyUpdates applies configuration updates to all registered components
func (r *ComponentRegistry) ApplyUpdates(oldConfig, newConfig *Config) {
	// Update debug mode/logging
	if oldConfig.Debug != newConfig.Debug {
		if r.Logging != nil {
			if err := r.Logging.UpdateDebugMode(newConfig.Debug); err != nil {
				r.logger.Error("Failed to update debug mode", "err", err)
			} else {
				r.logger.Info("Debug mode updated successfully", "debug", newConfig.Debug)
			}
		}
	}

	// Update download workers (now in streaming config)
	if oldConfig.Streaming.MaxDownloadWorkers != newConfig.Streaming.MaxDownloadWorkers {
		if r.Metadata != nil {
			if err := r.Metadata.UpdateMaxDownloadWorkers(newConfig.Streaming.MaxDownloadWorkers); err != nil {
				r.logger.Error("Failed to update download workers", "err", err)
			} else {
				r.logger.Info("Download workers updated successfully",
					"old", oldConfig.Streaming.MaxDownloadWorkers,
					"new", newConfig.Streaming.MaxDownloadWorkers)
			}
		}
	}

	// Update import processor workers
	if oldConfig.Import.MaxProcessorWorkers != newConfig.Import.MaxProcessorWorkers {
		if r.WorkerPool != nil {
			if err := r.WorkerPool.UpdateImportWorkers(newConfig.Import.MaxProcessorWorkers); err != nil {
				r.logger.Error("Failed to update import processor workers", "err", err)
			} else {
				r.logger.Info("Import processor workers updated successfully",
					"old", oldConfig.Import.MaxProcessorWorkers,
					"new", newConfig.Import.MaxProcessorWorkers)
			}
		}
	}

	// Update WebDAV authentication
	if oldConfig.WebDAV.User != newConfig.WebDAV.User || oldConfig.WebDAV.Password != newConfig.WebDAV.Password {
		if r.WebDAV != nil {
			if err := r.WebDAV.UpdateAuth(newConfig.WebDAV.User, newConfig.WebDAV.Password); err != nil {
				r.logger.Error("Failed to update WebDAV authentication", "err", err)
			} else {
				r.logger.Info("WebDAV authentication updated successfully")
			}
		}
	}

	// API authentication is now handled by OAuth flow only

	// Update NZB directory
	if oldConfig.WatchPath != newConfig.WatchPath {
		if r.Directory != nil {
			if err := r.Directory.UpdateWatchPathectory(newConfig.WatchPath); err != nil {
				r.logger.Error("Failed to update NZB directory", "err", err)
			} else {
				r.logger.Info("NZB directory updated successfully",
					"old", oldConfig.WatchPath,
					"new", newConfig.WatchPath)
			}
		}
	}

	// Update RClone settings
	if oldConfig.RClone.Password != newConfig.RClone.Password || oldConfig.RClone.Salt != newConfig.RClone.Salt {
		if r.RClone != nil {
			if err := r.RClone.UpdateRCloneSettings(newConfig.RClone.Password, newConfig.RClone.Salt); err != nil {
				r.logger.Error("Failed to update RClone settings", "err", err)
			} else {
				r.logger.Info("RClone settings updated successfully")
			}
		}
	}

	// Update streaming settings
	if oldConfig.Streaming.MaxRangeSize != newConfig.Streaming.MaxRangeSize {
		if r.Metadata != nil {
			if err := r.Metadata.UpdateMaxRangeSize(newConfig.Streaming.MaxRangeSize); err != nil {
				r.logger.Error("Failed to update max range size", "err", err)
			} else {
				r.logger.Info("Max range size updated successfully",
					"old", oldConfig.Streaming.MaxRangeSize,
					"new", newConfig.Streaming.MaxRangeSize)
			}
		}
	}

	if oldConfig.Streaming.StreamingChunkSize != newConfig.Streaming.StreamingChunkSize {
		if r.Metadata != nil {
			if err := r.Metadata.UpdateStreamingChunkSize(newConfig.Streaming.StreamingChunkSize); err != nil {
				r.logger.Error("Failed to update streaming chunk size", "err", err)
			} else {
				r.logger.Info("Streaming chunk size updated successfully",
					"old", oldConfig.Streaming.StreamingChunkSize,
					"new", newConfig.Streaming.StreamingChunkSize)
			}
		}
	}
}
