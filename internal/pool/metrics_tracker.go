package pool

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/nntppool/v4"
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
	pool              *nntppool.Client
	mu                sync.RWMutex
	samples           []metricsample
	sampleInterval    time.Duration
	retentionPeriod   time.Duration
	calculationWindow time.Duration // Window for speed calculations (shorter than retention for accuracy)
	maxDownloadSpeed  float64
	cancel            context.CancelFunc
	logger            *slog.Logger
}

// metricsample represents a single metrics sample at a point in time
type metricsample struct {
	avgSpeed       float64
	totalErrors    int64
	providerErrors map[string]int64
	timestamp      time.Time
}

// NewMetricsTracker creates a new metrics tracker
func NewMetricsTracker(pool *nntppool.Client) *MetricsTracker {
	mt := &MetricsTracker{
		pool:              pool,
		samples:           make([]metricsample, 0, 60), // Preallocate for 60 samples
		sampleInterval:    5 * time.Second,
		retentionPeriod:   60 * time.Second,
		calculationWindow: 10 * time.Second, // Use 10s window for more accurate real-time speeds
		logger:            slog.Default().With("component", "metrics-tracker"),
	}

	return mt
}

// Start begins collecting metrics samples
func (mt *MetricsTracker) Start(ctx context.Context) {
	childCtx, cancel := context.WithCancel(ctx)
	mt.cancel = cancel

	// Take initial sample
	mt.takeSample()

	// Start sampling goroutine
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
		mt.logger.InfoContext(context.Background(), "Metrics tracker stopped")
	}
}

// GetSnapshot returns the current metrics with calculated speeds
func (mt *MetricsTracker) GetSnapshot() MetricsSnapshot {
	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Get current stats from pool
	stats := mt.pool.Stats()
	now := time.Now()

	// Calculate total errors and provider errors from v4 stats
	var totalErrors int64
	providerErrors := make(map[string]int64)
	for _, ps := range stats.Providers {
		totalErrors += ps.Errors
		providerErrors[ps.Name] = ps.Errors
	}

	// v4 provides AvgSpeed directly (bytes/sec)
	downloadSpeed := stats.AvgSpeed

	// Update max speed
	if downloadSpeed > mt.maxDownloadSpeed {
		mt.maxDownloadSpeed = downloadSpeed
	}

	// Approximate bytes downloaded from average speed and elapsed time
	var bytesDownloaded int64
	if stats.Elapsed > 0 {
		bytesDownloaded = int64(stats.AvgSpeed * stats.Elapsed.Seconds())
	}

	return MetricsSnapshot{
		BytesDownloaded:             bytesDownloaded,
		BytesUploaded:               0, // v4 doesn't track uploads
		ArticlesDownloaded:          0, // v4 doesn't track article counts
		ArticlesPosted:              0, // v4 doesn't track article counts
		TotalErrors:                 totalErrors,
		ProviderErrors:              providerErrors,
		DownloadSpeedBytesPerSec:    downloadSpeed,
		MaxDownloadSpeedBytesPerSec: mt.maxDownloadSpeed,
		UploadSpeedBytesPerSec:      0, // v4 doesn't track uploads
		Timestamp:                   now,
	}
}

// samplingLoop periodically samples metrics
func (mt *MetricsTracker) samplingLoop(ctx context.Context) {
	ticker := time.NewTicker(mt.sampleInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			mt.takeSample()
		}
	}
}

// takeSample captures a metrics snapshot and stores it
func (mt *MetricsTracker) takeSample() {
	stats := mt.pool.Stats()

	mt.mu.Lock()
	defer mt.mu.Unlock()

	// Calculate total errors and provider errors
	var totalErrors int64
	providerErrors := make(map[string]int64)
	for _, ps := range stats.Providers {
		totalErrors += ps.Errors
		providerErrors[ps.Name] = ps.Errors
	}

	// Create sample
	sample := metricsample{
		avgSpeed:       stats.AvgSpeed,
		totalErrors:    totalErrors,
		providerErrors: copyProviderErrors(providerErrors),
		timestamp:      time.Now(),
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
