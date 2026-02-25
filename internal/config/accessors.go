package config

import "time"

// Health config accessor methods with default fallbacks.
// These methods provide safe access to health configuration values
// with sensible defaults when values are not set or invalid.

// GetCheckInterval returns the health check interval with a default fallback.
func (c *Config) GetCheckInterval() time.Duration {
	if c.Health.CheckIntervalSeconds <= 0 {
		return 5 * time.Second // Default: 5 seconds
	}
	return time.Duration(c.Health.CheckIntervalSeconds) * time.Second
}

// GetMaxConcurrentJobs returns max concurrent health check jobs with a default fallback.
func (c *Config) GetMaxConcurrentJobs() int {
	if c.Health.MaxConcurrentJobs <= 0 {
		return 1 // Default: 1 job
	}
	return c.Health.MaxConcurrentJobs
}

// GetMaxConnectionsForHealthChecks returns max connections for health checks with a default fallback.
func (c *Config) GetMaxConnectionsForHealthChecks() int {
	if c.Health.MaxConnectionsForHealthChecks <= 0 {
		return 5 // Default: 5 connections
	}
	return c.Health.MaxConnectionsForHealthChecks
}

// GetSegmentSamplePercentage returns segment sample percentage with a default fallback.
// Returns a value between 1 and 100.
func (c *Config) GetSegmentSamplePercentage() int {
	if c.Health.SegmentSamplePercentage < 1 || c.Health.SegmentSamplePercentage > 100 {
		return 5 // Default: 5%
	}
	return c.Health.SegmentSamplePercentage
}

// GetLibrarySyncInterval returns the library sync interval with a default fallback.
func (c *Config) GetLibrarySyncInterval() time.Duration {
	if c.Health.LibrarySyncIntervalMinutes <= 0 {
		return 60 * time.Minute // Default: 60 minutes
	}
	return time.Duration(c.Health.LibrarySyncIntervalMinutes) * time.Minute
}

// GetLibrarySyncConcurrency returns the library sync concurrency with a default fallback.
func (c *Config) GetLibrarySyncConcurrency() int {
	if c.Health.LibrarySyncConcurrency <= 0 {
		return 5 // Default: 5 concurrent operations
	}
	return c.Health.LibrarySyncConcurrency
}

// GetVerifyData returns whether to verify data during health checks.
func (c *Config) GetVerifyData() bool {
	if c.Health.VerifyData == nil {
		return false // Default: false
	}
	return *c.Health.VerifyData
}

// GetCheckAllSegments returns whether to check all segments during health checks.
func (c *Config) GetCheckAllSegments() bool {
	if c.Health.CheckAllSegments == nil {
		return false // Default: false
	}
	return *c.Health.CheckAllSegments
}

// Import config accessor methods.

// GetMaxImportConnections returns max import connections with a default fallback.
func (c *Config) GetMaxImportConnections() int {
	if c.Import.MaxImportConnections <= 0 {
		return 5 // Default: 5 connections
	}
	return c.Import.MaxImportConnections
}

// GetMaxDownloadPrefetch returns max download prefetch with a default fallback.
func (c *Config) GetMaxDownloadPrefetch() int {
	if c.Import.MaxDownloadPrefetch <= 0 {
		return 3 // Default: 3 segments prefetched ahead
	}
	return c.Import.MaxDownloadPrefetch
}

// GetReadTimeoutSeconds returns read timeout in seconds with a default fallback.
func (c *Config) GetReadTimeoutSeconds() int {
	if c.Import.ReadTimeoutSeconds <= 0 {
		return 30 // Default: 30 seconds
	}
	return c.Import.ReadTimeoutSeconds
}

// GetReadTimeout returns read timeout as a duration with a default fallback.
func (c *Config) GetReadTimeout() time.Duration {
	return time.Duration(c.GetReadTimeoutSeconds()) * time.Second
}

// GetMetadataBackupInterval returns the metadata backup interval with a default fallback.
func (c *Config) GetMetadataBackupInterval() time.Duration {
	if c.Metadata.Backup.IntervalHours <= 0 {
		return 24 * time.Hour // Default: 24 hours
	}
	return time.Duration(c.Metadata.Backup.IntervalHours) * time.Hour
}

// GetMetadataBackupKeep returns the number of metadata backups to keep with a default fallback.
func (c *Config) GetMetadataBackupKeep() int {
	if c.Metadata.Backup.KeepBackups <= 0 {
		return 10 // Default: 10 backups
	}
	return c.Metadata.Backup.KeepBackups
}

// GetFuseMountPath returns the FUSE mount path, falling back to the root mount_path if not set.
func (c *Config) GetFuseMountPath() string {
	if c.Fuse.MountPath != "" {
		return c.Fuse.MountPath
	}
	return c.MountPath
}

// GetHealthEnabled returns whether health checking is enabled (defaults to true)
func (c *Config) GetHealthEnabled() bool {
	if c.Health.Enabled == nil {
		return true
	}
	return *c.Health.Enabled
}
