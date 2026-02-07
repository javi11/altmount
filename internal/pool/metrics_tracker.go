package pool

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/nntppool/v2"
)

// MetricsSnapshot represents pool metrics at a point in time with calculated values
type MetricsSnapshot struct {
	BytesDownloaded             int64            `json:"bytes_downloaded"`
	BytesUploaded               int64            `json:"bytes_uploaded"`
	ArticlesDownloaded          int64            `json:"articles_downloaded"`
	ArticlesPosted              int64            `json:"articles_posted"`
	TotalErrors                 int64            `json:"total_errors"`
	ProviderErrors              map[string]int64 `json:"provider_errors"`
	DownloadSpeedBytesPerSec    float64          `json:"download_speed_bytes_per_sec"`
	MaxDownloadSpeedBytesPerSec float64          `json:"max_download_speed_bytes_per_sec"`
	UploadSpeedBytesPerSec      float64          `json:"upload_speed_bytes_per_sec"`
	Timestamp                   time.Time        `json:"timestamp"`
}

// MetricsTracker tracks pool metrics over time and calculates rates
type MetricsTracker struct {
	pool              nntppool.UsenetConnectionPool
	repo              *database.Repository
	mu                sync.RWMutex
	persistenceMu     sync.Mutex // Mutex for persistent state updates (offsets)
	samples           []metricsample
	sampleInterval    time.Duration
	retentionPeriod   time.Duration
	calculationWindow time.Duration // Window for speed calculations (shorter than retention for accuracy)
	maxDownloadSpeed  float64
	offsetBytes       int64
	offsetArticles    int64
	cancel            context.CancelFunc
	logger            *slog.Logger
}

// metricsample represents a single metrics sample at a point in time
type metricsample struct {
	bytesDownloaded    int64
	bytesUploaded      int64
	articlesDownloaded int64
	articlesPosted     int64
	totalErrors        int64
	providerErrors     map[string]int64
	timestamp          time.Time
}

// NewMetricsTracker creates a new metrics tracker
func NewMetricsTracker(pool nntppool.UsenetConnectionPool, repo *database.Repository) *MetricsTracker {
	mt := &MetricsTracker{
		pool:              pool,
		repo:              repo,
		samples:           make([]metricsample, 0, 60), // Preallocate for 60 samples
		sampleInterval:    5 * time.Second,
		retentionPeriod:   60 * time.Second,
		calculationWindow: 10 * time.Second, // Use 10s window for more accurate real-time speeds
		logger:            slog.Default().With("component", "metrics-tracker"),
	}

	// Initialize persistent offsets from database
	mt.loadPersistentStats()

	return mt
}

func (mt *MetricsTracker) loadPersistentStats() {
	if mt.repo == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	mt.persistenceMu.Lock()
	defer mt.persistenceMu.Unlock()

	bytes, err := mt.repo.GetSystemStat(ctx, "bytes_downloaded")
	if err != nil {
		mt.logger.Error("Failed to load persistent bytes_downloaded", "err", err)
	} else {
		mt.offsetBytes = bytes
	}

	articles, err := mt.repo.GetSystemStat(ctx, "articles_downloaded")
	if err != nil {
		mt.logger.Error("Failed to load persistent articles_downloaded", "err", err)
	} else {
		mt.offsetArticles = articles
	}

	maxSpeed, err := mt.repo.GetSystemStat(ctx, "max_download_speed")
	if err != nil {
		mt.logger.Error("Failed to load persistent max_download_speed", "err", err)
	} else {
		mt.maxDownloadSpeed = float64(maxSpeed)
	}
}

// Start begins collecting metrics samples
func (mt *MetricsTracker) Start(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	mt.cancel = cancel

	// Take initial sample
	mt.takeSample()

	// Start sampling goroutine
	go mt.samplingLoop(childCtx)

	mt.mu.RLock()
	mt.persistenceMu.Lock()
	mt.logger.InfoContext(ctx, "Metrics tracker started",
		"sample_interval", mt.sampleInterval,
		"retention_period", mt.retentionPeriod,
		"offset_bytes", mt.offsetBytes,
		"offset_articles", mt.offsetArticles,
	)
	mt.persistenceMu.Unlock()
	mt.mu.RUnlock()
}

// Stop stops collecting metrics samples
func (mt *MetricsTracker) Stop() {
	if mt.cancel != nil {
		mt.cancel()

		// Save final metrics before stopping
		mt.savePersistentStats(context.Background())

		mt.logger.InfoContext(context.Background(), "Metrics tracker stopped")
	}
}

// GetSnapshot returns the current metrics with calculated speeds
func (mt *MetricsTracker) GetSnapshot() MetricsSnapshot {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Get current snapshot from pool
	poolSnapshot := mt.pool.GetMetricsSnapshot()

	// Calculate speeds
	downloadSpeed, uploadSpeed := mt.calculateSpeeds(poolSnapshot)

	// Update max speed
	if downloadSpeed > mt.maxDownloadSpeed {
		mt.maxDownloadSpeed = downloadSpeed
	}

	mt.persistenceMu.Lock()
	defer mt.persistenceMu.Unlock()

	return MetricsSnapshot{
		BytesDownloaded:             mt.offsetBytes + poolSnapshot.BytesDownloaded,
		BytesUploaded:               poolSnapshot.BytesUploaded,
		ArticlesDownloaded:          mt.offsetArticles + poolSnapshot.ArticlesDownloaded,
		ArticlesPosted:              poolSnapshot.ArticlesPosted,
		TotalErrors:                 poolSnapshot.TotalErrors,
		ProviderErrors:              poolSnapshot.ProviderErrors,
		DownloadSpeedBytesPerSec:    downloadSpeed,
		MaxDownloadSpeedBytesPerSec: mt.maxDownloadSpeed,
		UploadSpeedBytesPerSec:      uploadSpeed,
		Timestamp:                   poolSnapshot.Timestamp,
	}
}

// UpdatePersistence allows external triggers to save persistent stats
func (mt *MetricsTracker) UpdatePersistence(ctx context.Context) {
	mt.savePersistentStats(ctx)
}

// ResetStats resets the cumulative statistics in memory and triggers a DB reset
func (mt *MetricsTracker) ResetStats(ctx context.Context) error {
	if mt.repo != nil {
		if err := mt.repo.ResetSystemStats(ctx); err != nil {
			return err
		}
	}

	// Reset in-memory state
	mt.mu.Lock()
	mt.samples = make([]metricsample, 0, 60)
	mt.maxDownloadSpeed = 0
	mt.mu.Unlock()

	mt.persistenceMu.Lock()
	// To fully reset, we must also reset the offsets and account for current pool session data
	// We can't easily reset nntppool's internal counters without recreating it, 
	// so we'll just adjust offsets to make the sum zero.
	snapshot := mt.pool.GetMetricsSnapshot()
	mt.offsetBytes = -snapshot.BytesDownloaded
	mt.offsetArticles = -snapshot.ArticlesDownloaded
	mt.persistenceMu.Unlock()

	mt.logger.InfoContext(ctx, "Cumulative statistics reset")
	return nil
}

// samplingLoop periodically samples metrics
func (mt *MetricsTracker) samplingLoop(ctx context.Context) {
	ticker := time.NewTicker(mt.sampleInterval)
	defer ticker.Stop()

	// Periodic persistence ticker (every 1 minute)
	persistTicker := time.NewTicker(1 * time.Minute)
	defer persistTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mt.takeSample()
		case <-persistTicker.C:
			mt.savePersistentStats(ctx)
		}
	}
}

// savePersistentStats persists current total stats to database
func (mt *MetricsTracker) savePersistentStats(ctx context.Context) {
	if mt.repo == nil {
		return
	}

	mt.mu.RLock()
	poolSnapshot := mt.pool.GetMetricsSnapshot()
	currentMaxSpeed := mt.maxDownloadSpeed
	mt.mu.RUnlock()

	mt.persistenceMu.Lock()
	totalBytes := mt.offsetBytes + poolSnapshot.BytesDownloaded
	totalArticles := mt.offsetArticles + poolSnapshot.ArticlesDownloaded
	mt.persistenceMu.Unlock()

	// Update in database
	if err := mt.repo.UpdateSystemStat(ctx, "bytes_downloaded", totalBytes); err != nil {
		mt.logger.ErrorContext(ctx, "Failed to persist bytes_downloaded", "err", err)
	}
	if err := mt.repo.UpdateSystemStat(ctx, "articles_downloaded", totalArticles); err != nil {
		mt.logger.ErrorContext(ctx, "Failed to persist articles_downloaded", "err", err)
	}
	if err := mt.repo.UpdateSystemStat(ctx, "max_download_speed", int64(currentMaxSpeed)); err != nil {
		mt.logger.ErrorContext(ctx, "Failed to persist max_download_speed", "err", err)
	}
}


// takeSample captures a metrics snapshot and stores it
func (mt *MetricsTracker) takeSample() {
	snapshot := mt.pool.GetMetricsSnapshot()

	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Create sample
	sample := metricsample{
		bytesDownloaded:    snapshot.BytesDownloaded,
		bytesUploaded:      snapshot.BytesUploaded,
		articlesDownloaded: snapshot.ArticlesDownloaded,
		articlesPosted:     snapshot.ArticlesPosted,
		totalErrors:        snapshot.TotalErrors,
		providerErrors:     copyProviderErrors(snapshot.ProviderErrors),
		timestamp:          snapshot.Timestamp,
	}

	// Add sample
	mt.samples = append(mt.samples, sample)

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

// calculateSpeeds calculates download and upload speeds based on historical samples
// Uses calculationWindow (default 10s) for more accurate real-time speed measurements
func (mt *MetricsTracker) calculateSpeeds(current nntppool.PoolMetricsSnapshot) (downloadSpeed, uploadSpeed float64) {
	// Need at least 2 samples to calculate speed
	if len(mt.samples) < 2 {
		return 0, 0
	}

	// Find sample closest to calculationWindow ago (instead of using oldest sample)
	// This provides more accurate real-time speed by looking at recent history
	targetTime := current.Timestamp.Add(-mt.calculationWindow)
	compareIndex := 0

	// Search backwards to find the sample closest to calculationWindow ago
	for i := len(mt.samples) - 1; i >= 0; i-- {
		if mt.samples[i].timestamp.Before(targetTime) || mt.samples[i].timestamp.Equal(targetTime) {
			compareIndex = i
			break
		}
	}

	compareSample := mt.samples[compareIndex]

	// Calculate time delta
	timeDelta := current.Timestamp.Sub(compareSample.timestamp).Seconds()
	if timeDelta <= 0 {
		return 0, 0
	}

	// Calculate download speed (bytes per second) over the calculation window
	bytesDelta := current.BytesDownloaded - compareSample.bytesDownloaded
	if bytesDelta > 0 {
		downloadSpeed = float64(bytesDelta) / timeDelta
	}

	// Calculate upload speed (bytes per second) over the calculation window
	uploadDelta := current.BytesUploaded - compareSample.bytesUploaded
	if uploadDelta > 0 {
		uploadSpeed = float64(uploadDelta) / timeDelta
	}

	return downloadSpeed, uploadSpeed
}

// copyProviderErrors creates a copy of the provider errors map
func copyProviderErrors(original map[string]int64) map[string]int64 {
	if original == nil {
		return nil
	}

	copy := make(map[string]int64, len(original))
	for k, v := range original {
		copy[k] = v
	}
	return copy
}
