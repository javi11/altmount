package pool

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/javi11/nntppool/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

type MockUsenetConnectionPool struct {
	mock.Mock
	nntppool.UsenetConnectionPool
}

func (m *MockUsenetConnectionPool) GetMetricsSnapshot() nntppool.PoolMetricsSnapshot {
	args := m.Called()
	return args.Get(0).(nntppool.PoolMetricsSnapshot)
}

func TestMetricsTracker_CumulativeStats(t *testing.T) {
	mockPool := new(MockUsenetConnectionPool)
	
	// Initial snapshot
	snapshot1 := nntppool.PoolMetricsSnapshot{
		BytesDownloaded:    1000,
		ArticlesDownloaded: 10,
		Timestamp:          time.Now(),
	}
	
	mockPool.On("GetMetricsSnapshot").Return(snapshot1).Once()
	
	mt := &MetricsTracker{
		pool:           mockPool,
		offsetBytes:    5000,
		offsetArticles: 50,
		logger:         slog.Default(),
	}
	
	// Test snapshot with offsets
	res := mt.GetSnapshot()
	assert.Equal(t, int64(6000), res.BytesDownloaded)
	assert.Equal(t, int64(60), res.ArticlesDownloaded)
	
	// Test Reset
	// Reset should set offsets such that (offset + current) = 0
	// ResetStats calls GetMetricsSnapshot internally
	mockPool.On("GetMetricsSnapshot").Return(snapshot1).Once()
	err := mt.ResetStats(context.Background())
	assert.NoError(t, err)
	
	// Mock pool snapshot after reset (still 1000/10 until pool itself is cleared)
	mockPool.On("GetMetricsSnapshot").Return(snapshot1).Once()
	
	res2 := mt.GetSnapshot()
	assert.Equal(t, int64(0), res2.BytesDownloaded)
	assert.Equal(t, int64(0), res2.ArticlesDownloaded)
	
	mockPool.AssertExpectations(t)
}
