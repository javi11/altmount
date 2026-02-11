package pool

import (
	"testing"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
)

func TestMetricsTracker_WindowedSpeed(t *testing.T) {
	mt := &MetricsTracker{
		samples:           make([]metricsample, 0),
		calculationWindow: 10 * time.Second,
	}

	now := time.Now()

	// Case 1: No samples
	snapshot := mt.getSnapshot(now, nntppool.ClientStats{})
	assert.Equal(t, 0.0, snapshot.DownloadSpeedBytesPerSec)

	// Case 2: One sample (100MB at now-5s)
	mt.samples = append(mt.samples, metricsample{
		totalBytes: 100 * 1024 * 1024,
		timestamp:  now.Add(-5 * time.Second),
	})

	// Current state: 150MB
	mt.liveBytesDownloaded.Store(150 * 1024 * 1024)
	
	snapshot = mt.getSnapshot(now, nntppool.ClientStats{})
	// Speed = (150 - 100) / 5 = 10 MB/s
	assert.Equal(t, float64(50*1024*1024)/5.0, snapshot.DownloadSpeedBytesPerSec)

	// Case 3: Multiple samples, all newer than calculationWindow
	mt.samples = append(mt.samples, metricsample{
		totalBytes: 120 * 1024 * 1024,
		timestamp:  now.Add(-2 * time.Second),
	})
	// Sample 0: 100MB at now-5s
	// Sample 1: 120MB at now-2s
	// cutoff = now-10s. Both are after cutoff. Fallback to oldest (Sample 0).
	
	snapshot = mt.getSnapshot(now, nntppool.ClientStats{})
	assert.Equal(t, float64(50*1024*1024)/5.0, snapshot.DownloadSpeedBytesPerSec)

	// Case 4: Sample older than 10s
	mt.samples = append([]metricsample{{
		totalBytes: 50 * 1024 * 1024,
		timestamp:  now.Add(-15 * time.Second),
	}}, mt.samples...)
	// Sample 0: 50MB at now-15s (Reference! It's the newest sample BEFORE now-10s)
	// Sample 1: 100MB at now-5s
	// Sample 2: 120MB at now-2s
	
	snapshot = mt.getSnapshot(now, nntppool.ClientStats{})
	// Speed = (150 - 50) / 15 = 6.66 MB/s
	assert.InDelta(t, float64(100*1024*1024)/15.0, snapshot.DownloadSpeedBytesPerSec, 0.001)
}
