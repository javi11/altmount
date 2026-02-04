package usenet

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v2"
	"github.com/javi11/nntppool/v2/pkg/nntpcli"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPoolManager
type MockPoolManager struct {
	mock.Mock
}

func (m *MockPoolManager) GetPool() (nntppool.UsenetConnectionPool, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(nntppool.UsenetConnectionPool), args.Error(1)
}

func (m *MockPoolManager) SetProviders(providers []nntppool.UsenetProviderConfig) error {
	return nil
}
func (m *MockPoolManager) ClearPool() error {
	return nil
}
func (m *MockPoolManager) HasPool() bool {
	return true
}
func (m *MockPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	return pool.MetricsSnapshot{}, nil
}

// MockConnectionPool
type MockConnectionPool struct {
	mock.Mock
}

func (m *MockConnectionPool) Stat(ctx context.Context, id string, groups []string) (int, error) {
	args := m.Called(ctx, id, groups)
	return args.Int(0), args.Error(1)
}

func (m *MockConnectionPool) Body(ctx context.Context, id string, w io.Writer, groups []string) (int64, error) {
	args := m.Called(ctx, id, w, groups)
	return int64(args.Int(0)), args.Error(1)
}

func (m *MockConnectionPool) BodyReader(ctx context.Context, id string, groups []string) (nntpcli.ArticleBodyReader, error) {
	args := m.Called(ctx, id, groups)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(nntpcli.ArticleBodyReader), args.Error(1)
}

func (m *MockConnectionPool) GetConnection(ctx context.Context, skipProviders []string, useBackupProviders bool) (nntppool.PooledConnection, error) {
	return nil, nil
}

func (m *MockConnectionPool) Post(ctx context.Context, r io.Reader) error {
	return nil
}

func (m *MockConnectionPool) BodyBatch(ctx context.Context, group string, requests []nntppool.BodyBatchRequest) []nntppool.BodyBatchResult {
	return nil
}

func (m *MockConnectionPool) TestProviderPipelineSupport(ctx context.Context, providerHost string, testMsgID string) (bool, int, error) {
	return false, 0, nil
}

func (m *MockConnectionPool) GetProvidersInfo() []nntppool.ProviderInfo {
	return nil
}

func (m *MockConnectionPool) GetProviderStatus(providerID string) (*nntppool.ProviderInfo, bool) {
	return nil, false
}

func (m *MockConnectionPool) GetMetrics() *nntppool.PoolMetrics {
	return nil
}

func (m *MockConnectionPool) GetMetricsSnapshot() nntppool.PoolMetricsSnapshot {
	return nntppool.PoolMetricsSnapshot{}
}

func (m *MockConnectionPool) Quit() {}

func TestValidateSegmentAvailabilityDetailed_Hybrid(t *testing.T) {
	// Setup
	mockPool := new(MockConnectionPool)
	mockManager := new(MockPoolManager)
	mockManager.On("GetPool").Return(mockPool, nil)

	segments := []*metapb.SegmentData{
		{Id: "seg1", SegmentSize: 1000},
	}

	// Test hybrid mode (verifyData = true)
	// Expect Body call
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, mock.Anything).Return(0, ErrLimitReached).Once()

	result, err := ValidateSegmentAvailabilityDetailed(
		context.Background(),
		segments,
		mockManager,
		1,
		100,
		nil,
		time.Second,
		true, // verifyData
	)

	assert.NoError(t, err)
	assert.Equal(t, 0, result.MissingCount)
	assert.Equal(t, 1, result.TotalChecked)

	mockPool.AssertExpectations(t)
	mockManager.AssertExpectations(t)
}

func TestValidateSegmentAvailabilityDetailed_Hybrid_Failure(t *testing.T) {
	// Setup
	mockPool := new(MockConnectionPool)
	mockManager := new(MockPoolManager)
	mockManager.On("GetPool").Return(mockPool, nil)

	segments := []*metapb.SegmentData{
		{Id: "seg1", SegmentSize: 1000},
	}

	// Test hybrid mode (verifyData = true)
	// Expect Body call returning generic error (e.g. 430 No Such Article)
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, mock.Anything).Return(0, fmt.Errorf("article not found")).Once()

	result, err := ValidateSegmentAvailabilityDetailed(
		context.Background(),
		segments,
		mockManager,
		1,
		100,
		nil,
		time.Second,
		true, // verifyData
	)

	// Our function accumulates errors internally and returns result
	assert.NoError(t, err)
	assert.Equal(t, 1, result.MissingCount)
	assert.Equal(t, 1, result.TotalChecked)

	mockPool.AssertExpectations(t)
	mockManager.AssertExpectations(t)
}