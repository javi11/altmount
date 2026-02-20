package pool

import (
	"context"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/javi11/nntppool/v4"
)

// MissingRateWarningThreshold is the missing articles per minute rate that triggers a warning.
const MissingRateWarningThreshold = 10.0

// MetricsSnapshot represents pool metrics at a point in time with calculated values
type MetricsSnapshot struct {
	BytesDownloaded             int64              `json:"bytes_downloaded"`
	BytesUploaded               int64              `json:"bytes_uploaded"`
	ArticlesDownloaded          int64              `json:"articles_downloaded"`
	ArticlesPosted              int64              `json:"articles_posted"`
	TotalErrors                 int64              `json:"total_errors"`
	ProviderErrors              map[string]int64   `json:"provider_errors"`
	DownloadSpeedBytesPerSec    float64            `json:"download_speed_bytes_per_sec"`
	MaxDownloadSpeedBytesPerSec float64            `json:"max_download_speed_bytes_per_sec"`
	UploadSpeedBytesPerSec      float64            `json:"upload_speed_bytes_per_sec"`
	Timestamp                   time.Time          `json:"timestamp"`
	ProviderMissingRates        map[string]float64 `json:"provider_missing_rates"`
	ProviderMissingWarning      map[string]bool    `json:"provider_missing_warning"`
}

// MetricsTracker tracks pool metrics over time and calculates rates
type MetricsTracker struct {
	pool              *nntppool.Client
	repo              StatsRepository
	mu                sync.RWMutex
	samples           []metricsample
	sampleInterval    time.Duration
	retentionPeriod   time.Duration
	calculationWindow time.Duration // Window for speed calculations (shorter than retention for accuracy)
	maxDownloadSpeed  float64
	// Live counters
	articlesDownloaded  atomic.Int64
	articlesPosted      atomic.Int64
	liveBytesDownloaded atomic.Int64
	// Persistent counters (loaded from DB on start)
	initialBytesDownloaded    int64
	initialArticlesDownloaded int64
	initialBytesUploaded      int64
	initialArticlesPosted     int64
	initialProviderErrors     map[string]int64
	lastSavedBytesDownloaded  int64
	persistenceThreshold      int64 // Bytes to download before forcing a save
	cancel                    context.CancelFunc
	wg                        sync.WaitGroup
	logger                    *slog.Logger
}

// metricsample represents a single metrics sample at a point in time
type metricsample struct {
	avgSpeed        float64
	totalBytes      int64
	totalErrors     int64
	providerErrors  map[string]int64
	providerMissing map[string]int64
	timestamp       time.Time
}

// NewMetricsTracker creates a new metrics tracker
func NewMetricsTracker(pool *nntppool.Client, repo StatsRepository) *MetricsTracker {
	mt := &MetricsTracker{
		pool:                  pool,
		repo:                  repo,
		samples:               make([]metricsample, 0, 60), // Preallocate for 60 samples
		initialProviderErrors: make(map[string]int64),
		sampleInterval:        2 * time.Second, // Match playback sampling for "live" feel
		retentionPeriod:       60 * time.Second,
		calculationWindow:     10 * time.Second,   // Use 10s window for more accurate real-time speeds
		persistenceThreshold:  1024 * 1024 * 1024, // Save every 1GB downloaded
		logger:                slog.Default().With("component", "metrics-tracker"),
	}

	return mt
}

// Start begins collecting metrics samples
func (mt *MetricsTracker) Start(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	mt.cancel = cancel

	// Load initial stats from DB
	if mt.repo != nil {
		stats, err := mt.repo.GetSystemStats(ctx)
		if err != nil {
			mt.logger.ErrorContext(ctx, "Failed to load system stats from database", "error", err)
		} else {
			mt.mu.Lock()
			mt.initialBytesDownloaded = stats["bytes_downloaded"]
			mt.initialArticlesDownloaded = stats["articles_downloaded"]
			mt.initialBytesUploaded = stats["bytes_uploaded"]
			mt.initialArticlesPosted = stats["articles_posted"]
			mt.maxDownloadSpeed = float64(stats["max_download_speed"])
			mt.lastSavedBytesDownloaded = mt.initialBytesDownloaded

			// Load provider errors (prefixed with provider_error:)
			for k, v := range stats {
				if after, ok := strings.CutPrefix(k, "provider_error:"); ok {
					providerID := after
					mt.initialProviderErrors[providerID] = v
				}
			}
			mt.mu.Unlock()

			mt.logger.InfoContext(ctx, "Loaded persistent system stats",
				"articles", mt.initialArticlesDownloaded,
				"bytes", mt.initialBytesDownloaded,
				"provider_stats", len(mt.initialProviderErrors))
		}
	}

	// Take initial sample
	mt.takeSample()

	// Start sampling goroutine
	mt.wg.Add(1)
	go mt.samplingLoop(childCtx)

	mt.logger.InfoContext(ctx, "Metrics tracker started",
		"sample_interval", mt.sampleInterval,
		"retention_period", mt.retentionPeriod,
	)
}

// Stop stops collecting metrics samples
func (mt *MetricsTracker) Stop() {
	if mt.cancel != nil {
		mt.cancel()
		mt.wg.Wait()
		mt.logger.InfoContext(context.Background(), "Metrics tracker stopped")
	}
}

// GetSnapshot returns the current metrics with calculated speeds
func (mt *MetricsTracker) GetSnapshot() MetricsSnapshot {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	return mt.getSnapshot(time.Now(), mt.pool.Stats())
}

func (mt *MetricsTracker) getSnapshot(now time.Time, stats nntppool.ClientStats) MetricsSnapshot {
	// Calculate total errors and provider errors from v4 stats
	var totalErrors int64
	providerErrors := make(map[string]int64)
	for _, ps := range stats.Providers {
		totalErrors += ps.Errors
		providerErrors[ps.Name] = ps.Errors
	}

	// Use live counter for speed and totals
	bytesDownloaded := mt.liveBytesDownloaded.Load()

	// Calculate windowed download speed
	downloadSpeed := 0.0
	if len(mt.samples) > 0 {
		// Find the sample closest to calculationWindow ago
		cutoff := now.Add(-mt.calculationWindow)
		var referenceSample *metricsample
		for i := len(mt.samples) - 1; i >= 0; i-- {
			if mt.samples[i].timestamp.Before(cutoff) {
				referenceSample = &mt.samples[i]
				break
			}
		}

		if referenceSample != nil {
			bytesDiff := bytesDownloaded - referenceSample.totalBytes
			duration := now.Sub(referenceSample.timestamp).Seconds()
			if duration > 0 && bytesDiff >= 0 {
				downloadSpeed = float64(bytesDiff) / duration
			}
		} else {
			// Fallback if we don't have enough samples yet
			// Use the oldest sample
			oldest := mt.samples[0]
			bytesDiff := bytesDownloaded - oldest.totalBytes
			duration := now.Sub(oldest.timestamp).Seconds()
			if duration > 0 && bytesDiff >= 0 {
				downloadSpeed = float64(bytesDiff) / duration
			}
		}
	}

	// Update max speed
	if downloadSpeed > mt.maxDownloadSpeed {
		mt.maxDownloadSpeed = downloadSpeed
	}

	// Merge provider errors
	mergedProviderErrors := make(map[string]int64)
	maps.Copy(mergedProviderErrors, mt.initialProviderErrors)
	for k, v := range providerErrors {
		mergedProviderErrors[k] += v
	}

	// Compute windowed missing article rates per provider
	missingRates := make(map[string]float64)
	missingWarning := make(map[string]bool)

	// Collect current missing counts from pool stats
	currentMissing := make(map[string]int64)
	for _, ps := range stats.Providers {
		currentMissing[ps.Name] = ps.Missing
	}

	// Find the oldest sample within the calculation window
	windowStart := now.Add(-mt.calculationWindow)
	var oldestSample *metricsample
	for i := range mt.samples {
		if mt.samples[i].timestamp.After(windowStart) || mt.samples[i].timestamp.Equal(windowStart) {
			oldestSample = &mt.samples[i]
			break
		}
	}

	if oldestSample != nil {
		elapsed := now.Sub(oldestSample.timestamp)
		if elapsed > 0 {
			elapsedMinutes := elapsed.Minutes()
			for name, current := range currentMissing {
				old := oldestSample.providerMissing[name]
				delta := current - old
				if delta > 0 && elapsedMinutes > 0 {
					rate := float64(delta) / elapsedMinutes
					missingRates[name] = rate
					missingWarning[name] = rate >= MissingRateWarningThreshold
				}
			}
		}
	}

	return MetricsSnapshot{
		BytesDownloaded:             bytesDownloaded + mt.initialBytesDownloaded,
		BytesUploaded:               mt.initialBytesUploaded,
		ArticlesDownloaded:          mt.articlesDownloaded.Load() + mt.initialArticlesDownloaded,
		ArticlesPosted:              mt.articlesPosted.Load() + mt.initialArticlesPosted,
		TotalErrors:                 totalErrors,
		ProviderErrors:              mergedProviderErrors,
		DownloadSpeedBytesPerSec:    downloadSpeed,
		MaxDownloadSpeedBytesPerSec: mt.maxDownloadSpeed,
		UploadSpeedBytesPerSec:      0, // v4 doesn't track uploads
		Timestamp:                   now,
		ProviderMissingRates:        missingRates,
		ProviderMissingWarning:      missingWarning,
	}
}

// IncArticlesDownloaded increments the count of articles successfully downloaded
func (mt *MetricsTracker) IncArticlesDownloaded() {
	mt.articlesDownloaded.Add(1)
}

// UpdateDownloadProgress updates the live bytes downloaded counter
func (mt *MetricsTracker) UpdateDownloadProgress(id string, bytesDownloaded int64) {
	mt.liveBytesDownloaded.Add(bytesDownloaded)
}

// IncArticlesPosted increments the count of articles successfully posted
func (mt *MetricsTracker) IncArticlesPosted() {
	mt.articlesPosted.Add(1)
}

// samplingLoop periodically samples metrics
func (mt *MetricsTracker) samplingLoop(ctx context.Context) {
	defer mt.wg.Done()
	ticker := time.NewTicker(mt.sampleInterval)
	defer ticker.Stop()

	// Use a longer interval for DB updates to avoid excessive writes
	dbUpdateTicker := time.NewTicker(1 * time.Minute)
	defer dbUpdateTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Final save on shutdown
			mt.saveStats(context.Background())
			return
		case <-ticker.C:
			mt.takeSample()
		case <-dbUpdateTicker.C:
			mt.saveStats(ctx)
		}
	}
}

// saveStats persists current totals to the database
func (mt *MetricsTracker) saveStats(ctx context.Context) {
	if mt.repo == nil {
		return
	}

	snapshot := mt.GetSnapshot()

	// Prepare batch update
	stats := map[string]int64{
		"bytes_downloaded":    snapshot.BytesDownloaded,
		"articles_downloaded": snapshot.ArticlesDownloaded,
		"bytes_uploaded":      snapshot.BytesUploaded,
		"articles_posted":     snapshot.ArticlesPosted,
		"max_download_speed":  int64(snapshot.MaxDownloadSpeedBytesPerSec),
	}

	// Add provider errors to batch
	for providerID, errorCount := range snapshot.ProviderErrors {
		stats["provider_error:"+providerID] = errorCount
	}

	if err := mt.repo.BatchUpdateSystemStats(ctx, stats); err != nil {
		mt.logger.ErrorContext(ctx, "Failed to persist system stats", "error", err)
	} else {
		// Calculate delta for daily stats
		mt.mu.Lock()
		delta := snapshot.BytesDownloaded - mt.lastSavedBytesDownloaded
		mt.lastSavedBytesDownloaded = snapshot.BytesDownloaded
		mt.mu.Unlock()

		// Update daily stats with the delta
		if delta > 0 {
			if err := mt.repo.AddBytesDownloadedToDailyStat(ctx, delta); err != nil {
				mt.logger.ErrorContext(ctx, "Failed to update daily download volume", "error", err)
			}
		}
	}
}

// Reset resets all cumulative metrics both in memory and in the database
func (mt *MetricsTracker) Reset(ctx context.Context) error {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	mt.initialBytesDownloaded = 0
	mt.initialArticlesDownloaded = 0
	mt.initialBytesUploaded = 0
	mt.initialArticlesPosted = 0
	mt.articlesDownloaded.Store(0)
	mt.articlesPosted.Store(0)
	mt.liveBytesDownloaded.Store(0)
	mt.maxDownloadSpeed = 0
	mt.initialProviderErrors = make(map[string]int64)

	// Clear samples to reset speed calculation
	mt.samples = make([]metricsample, 0, 60)

	// Persist the reset state to database
	if mt.repo != nil {
		// We need to fetch all current keys to know what to reset (especially provider errors)
		currentStats, err := mt.repo.GetSystemStats(ctx)
		if err != nil {
			mt.logger.ErrorContext(ctx, "Failed to fetch stats for reset", "error", err)
		} else {
			resetMap := make(map[string]int64)
			for k := range currentStats {
				resetMap[k] = 0
			}
			// Ensure core keys are present
			resetMap["bytes_downloaded"] = 0
			resetMap["articles_downloaded"] = 0
			resetMap["bytes_uploaded"] = 0
			resetMap["articles_posted"] = 0
			resetMap["max_download_speed"] = 0

			if err := mt.repo.BatchUpdateSystemStats(ctx, resetMap); err != nil {
				mt.logger.ErrorContext(ctx, "Failed to persist reset stats", "error", err)
			}
		}
	}

	mt.logger.InfoContext(ctx, "Pool metrics have been reset")
	return nil
}

// takeSample captures a metrics snapshot and stores it
func (mt *MetricsTracker) takeSample() {
	stats := mt.pool.Stats()

	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Calculate total errors, provider errors, and provider missing counts
	var totalErrors int64
	providerErrors := make(map[string]int64)
	providerMissing := make(map[string]int64)
	for _, ps := range stats.Providers {
		totalErrors += ps.Errors
		providerErrors[ps.Name] = ps.Errors
		providerMissing[ps.Name] = ps.Missing
	}

	bytesDownloaded := mt.liveBytesDownloaded.Load()

	// Create sample
	sample := metricsample{
		totalBytes:      bytesDownloaded,
		avgSpeed:        stats.AvgSpeed,
		totalErrors:     totalErrors,
		providerErrors:  copyProviderErrors(providerErrors),
		providerMissing: copyProviderErrors(providerMissing),
		timestamp:       time.Now(),
	}

	// Add sample
	mt.samples = append(mt.samples, sample)

	// Adaptive Persistence: Check if we should force a save due to high activity
	totalBytesDownloaded := bytesDownloaded + mt.initialBytesDownloaded
	if totalBytesDownloaded-mt.lastSavedBytesDownloaded >= mt.persistenceThreshold {
		// Use a non-blocking save or a shorter context?
		// For now, simple call is fine as it's a goroutine
		go mt.saveStats(context.Background())
	}

	// Clean up old samples
	mt.cleanupOldSamples()
}

// cleanupOldSamples removes samples older than the retention period
func (mt *MetricsTracker) cleanupOldSamples() {
	cutoff := time.Now().Add(-mt.retentionPeriod)

	// Find first sample to keep
	keepIndex := 0
	for i, sample := range mt.samples {
		if sample.timestamp.After(cutoff) {
			keepIndex = i
			break
		}
	}

	// Remove old samples
	if keepIndex > 0 {
		mt.samples = mt.samples[keepIndex:]
	}
}

// copyProviderErrors creates a copy of the provider errors map
func copyProviderErrors(original map[string]int64) map[string]int64 {
	if original == nil {
		return nil
	}

	copy := make(map[string]int64, len(original))
	maps.Copy(copy, original)
	return copy
}
