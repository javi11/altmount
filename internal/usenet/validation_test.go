package usenet

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPoolManager
type MockPoolManager struct {
	mock.Mock
}

func (m *MockPoolManager) GetPool() (nntppool.NNTPClient, error) {
	args := m.Called()
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(nntppool.NNTPClient), args.Error(1)
}

func (m *MockPoolManager) SetProviders(providers []config.ProviderConfig) error {
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

func (m *MockConnectionPool) Stat(ctx context.Context, id string) (*nntppool.Response, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*nntppool.Response), args.Error(1)
}

func (m *MockConnectionPool) Body(ctx context.Context, id string, w io.Writer) error {
	args := m.Called(ctx, id, w)
	return args.Error(0)
}

func (m *MockConnectionPool) BodyReader(ctx context.Context, id string) (nntppool.YencReader, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(nntppool.YencReader), args.Error(1)
}

func (m *MockConnectionPool) BodyAt(ctx context.Context, id string, w io.WriterAt) error {
	return nil
}

func (m *MockConnectionPool) Article(ctx context.Context, id string, w io.Writer) error {
	return nil
}

func (m *MockConnectionPool) Head(ctx context.Context, id string) (*nntppool.Response, error) {
	return nil, nil
}

func (m *MockConnectionPool) Group(ctx context.Context, group string) (*nntppool.Response, error) {
	return nil, nil
}

func (m *MockConnectionPool) Post(ctx context.Context, headers map[string]string, body io.Reader) (*nntppool.Response, error) {
	return nil, nil
}

func (m *MockConnectionPool) PostYenc(ctx context.Context, headers map[string]string, body io.Reader, opts *nntppool.YencOptions) (*nntppool.Response, error) {
	return nil, nil
}

func (m *MockConnectionPool) AddProvider(provider *nntppool.Provider, tier nntppool.ProviderType) error {
	return nil
}

func (m *MockConnectionPool) RemoveProvider(provider *nntppool.Provider) error {
	return nil
}

func (m *MockConnectionPool) Close() {}

func (m *MockConnectionPool) Send(ctx context.Context, payload []byte, bodyWriter io.Writer) <-chan nntppool.Response {
	return nil
}

func (m *MockConnectionPool) Metrics() map[string]nntppool.ProviderMetrics {
	return nil
}

func (m *MockConnectionPool) SpeedTest(ctx context.Context, articleIDs []string) (nntppool.SpeedTestStats, error) {
	return nntppool.SpeedTestStats{}, nil
}

func (m *MockConnectionPool) Date(ctx context.Context) error {
	return nil
}

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
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything).Return(ErrLimitReached).Once()

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
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything).Return(fmt.Errorf("article not found")).Once()

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
