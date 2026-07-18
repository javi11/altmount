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
		return 100 // Default: 100 concurrent STAT checks
	}
	return c.Health.MaxConnectionsForHealthChecks
}

// GetCheckBatchSize returns how many due files each health cycle fetches and
// sweeps together in one cross-file StatMany call, with a default fallback.
// Distinct from GetMaxConnectionsForHealthChecks: this bounds how many FILES
// are batched into one sweep, while that bounds how many segment checks run
// concurrently within a sweep.
func (c *Config) GetCheckBatchSize() int {
	if c.Health.CheckBatchSize <= 0 {
		return 50 // Default: 50 files per cycle
	}
	return c.Health.CheckBatchSize
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

// GetImportDamagePolicyTolerant reports whether small confirmed damage on a
// standalone video file should import as degraded (true, the default) instead
// of failing the import (false, "strict").
func (c *Config) GetImportDamagePolicyTolerant() bool {
	return c.Import.DamagePolicy != "strict"
}

// TotalProviderConnections returns the pool's total connection capacity: the
// sum of MaxConnections across enabled, non-backup providers. When no primary
// providers are configured it falls back to the enabled backup providers' sum
// so the capacity is not zero while a usable pool exists. Returns 0 when no
// providers are configured at all.
func (c *Config) TotalProviderConnections() int {
	primary := 0
	backup := 0
	for _, p := range c.Providers {
		if p.Enabled != nil && !*p.Enabled {
			continue
		}
		if p.IsBackupProvider != nil && *p.IsBackupProvider {
			backup += p.MaxConnections
		} else {
			primary += p.MaxConnections
		}
	}
	if primary > 0 {
		return primary
	}
	return backup
}

// GetMaxConcurrentImports returns the global cap on concurrent NZB imports.
// 0 means unlimited (the default).
func (c *Config) GetMaxConcurrentImports() int {
	if c.Import.MaxConcurrentImports < 0 {
		return 0
	}
	return c.Import.MaxConcurrentImports
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

// GetHealthExcludedCategories returns the SABnzbd category names whose files
// must not be registered for health checking by the library-sync pass.
func (c *Config) GetHealthExcludedCategories() []string {
	return c.Health.ExcludedCategories
}

// GetHealthDeleteOnCorruption reports whether confirmed corruption should delete the
// file instead of triggering an Arr repair (health.corruption_action == "delete").
func (c *Config) GetHealthDeleteOnCorruption() bool {
	return c.Health.CorruptionAction == "delete"
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
