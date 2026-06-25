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

// GetHealthReadTimeout returns the health check read timeout as a duration with a default fallback.
func (c *Config) GetHealthReadTimeout() time.Duration {
	if c.Health.ReadTimeoutSeconds <= 0 {
		return 30 * time.Second // Default: 30 seconds
	}
	return time.Duration(c.Health.ReadTimeoutSeconds) * time.Second
}

// Stream Check config accessors (POST /api/nzb/check) with default fallbacks.

// GetStreamCheckEnabled reports whether the on-demand availability-check endpoint is enabled.
func (c *Config) GetStreamCheckEnabled() bool {
	return c.StreamCheck.Enabled != nil && *c.StreamCheck.Enabled
}

// GetStreamCheckSamplePercentage returns the segment sample percentage (1-100) with a default fallback.
func (c *Config) GetStreamCheckSamplePercentage() int {
	if c.StreamCheck.SegmentSamplePercentage < 1 || c.StreamCheck.SegmentSamplePercentage > 100 {
		return 5 // Default: 5%
	}
	return c.StreamCheck.SegmentSamplePercentage
}

// GetStreamCheckMaxConnections returns the max concurrent STAT connections with a default fallback.
func (c *Config) GetStreamCheckMaxConnections() int {
	if c.StreamCheck.MaxConnections <= 0 {
		return 10 // Default: 10 connections
	}
	return c.StreamCheck.MaxConnections
}

// GetStreamCheckTimeout returns the per-segment STAT timeout as a duration with a default fallback.
func (c *Config) GetStreamCheckTimeout() time.Duration {
	if c.StreamCheck.TimeoutSeconds <= 0 {
		return 15 * time.Second // Default: 15 seconds
	}
	return time.Duration(c.StreamCheck.TimeoutSeconds) * time.Second
}

// GetStreamCheckAcceptableMissingPercentage returns the tolerated missing-segment percentage (0-100).
func (c *Config) GetStreamCheckAcceptableMissingPercentage() float64 {
	if c.StreamCheck.AcceptableMissingPercentage < 0 || c.StreamCheck.AcceptableMissingPercentage > 100 {
		return 0 // Default: no missing segments tolerated
	}
	return c.StreamCheck.AcceptableMissingPercentage
}

// GetStreamCheckCacheTTL returns the verdict cache TTL as a duration (0 disables caching).
func (c *Config) GetStreamCheckCacheTTL() time.Duration {
	if c.StreamCheck.CacheTTLMinutes <= 0 {
		return 0
	}
	return time.Duration(c.StreamCheck.CacheTTLMinutes) * time.Minute
}

// GetStreamCheckMaxBatch returns the max NZBs checkable per request with a default fallback.
func (c *Config) GetStreamCheckMaxBatch() int {
	if c.StreamCheck.MaxBatch <= 0 {
		return 50 // Default: 50 items
	}
	return c.StreamCheck.MaxBatch
}

// GetMaxRetries returns the maximum number of health check retries.
func (c *Config) GetMaxRetries() int {
	if c.Health.MaxRetries <= 0 {
		return 2 // Default: 2 retries
	}
	return c.Health.MaxRetries
}

// GetMaxRepairRetries returns the maximum number of repair notification retries.
func (c *Config) GetMaxRepairRetries() int {
	if c.Health.Repair.MaxRepairRetries <= 0 {
		return 3 // Default: 3 retries
	}
	return c.Health.Repair.MaxRepairRetries
}

// Import config accessor methods.

// GetMaxImportConnections returns max import connections with a default fallback.
func (c *Config) GetMaxImportConnections() int {
	if c.Import.MaxImportConnections <= 0 {
		return 5 // Default: 5 connections
	}
	return c.Import.MaxImportConnections
}

// GetMaxConcurrentImports returns the global cap on concurrent NZB imports
// when no stream is active. 0 means unlimited (current default behaviour).
func (c *Config) GetMaxConcurrentImports() int {
	if c.Import.MaxConcurrentImports < 0 {
		return 0
	}
	return c.Import.MaxConcurrentImports
}

// GetMaxConcurrentImportsWhileStreaming returns the cap on concurrent NZB
// imports while at least one stream is active. 0 means unlimited (current
// default behaviour).
func (c *Config) GetMaxConcurrentImportsWhileStreaming() int {
	if c.Import.MaxConcurrentImportsWhileStreaming < 0 {
		return 0
	}
	return c.Import.MaxConcurrentImportsWhileStreaming
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

// GetIsoAnalyzeTimeout returns the per-ISO analyse deadline with a 120s
// default fallback. This bounds the entire iso.AnalyzeISO walk so a
// degraded NNTP provider cannot stall the importer indefinitely.
//
// Sentinel handling:
//   - nil (config field unset)        → 120s default
//   - 0 or negative (explicit "none") → 120s default; users cannot disable
//     the cap — the whole purpose of this knob is to prevent unbounded
//     waits. To approximate "unlimited", set a very large value (e.g.
//     86400 for a one-day budget).
func (c *Config) GetIsoAnalyzeTimeout() time.Duration {
	if c.Import.IsoAnalyzeTimeoutSeconds == nil || *c.Import.IsoAnalyzeTimeoutSeconds <= 0 {
		return 120 * time.Second
	}
	return time.Duration(*c.Import.IsoAnalyzeTimeoutSeconds) * time.Second
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

// GetRepairEnabled returns whether automatic repair is enabled (defaults to true)
func (c *Config) GetRepairEnabled() bool {
	if c.Health.Repair.Enabled == nil {
		return true
	}
	return *c.Health.Repair.Enabled
}

// GetRepairInterval returns the repair check interval
func (c *Config) GetRepairInterval() time.Duration {
	if c.Health.Repair.IntervalMinutes <= 0 {
		return 60 * time.Minute // Default: 1 hour
	}
	return time.Duration(c.Health.Repair.IntervalMinutes) * time.Minute
}

// GetRepairMaxCoolDown returns the maximum cooldown for repairs
func (c *Config) GetRepairMaxCoolDown() time.Duration {
	if c.Health.Repair.MaxCoolDownHours <= 0 {
		return 24 * time.Hour // Default: 24 hours
	}
	return time.Duration(c.Health.Repair.MaxCoolDownHours) * time.Hour
}

// GetRepairExponentialBackoff returns whether exponential backoff is enabled for repairs
func (c *Config) GetRepairExponentialBackoff() bool {
	if c.Health.Repair.ExponentialBackoff == nil {
		return true
	}
	return *c.Health.Repair.ExponentialBackoff
}
