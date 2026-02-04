package usenet

import (
	"context"
	"fmt"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockConnectionPool implements nntppool.UsenetConnectionPool for testing
// We embed the interface so we don't have to implement all methods
type MockConnectionPool struct {
	mock.Mock
	nntppool.UsenetConnectionPool // Embedding the interface
}

// Implement only the methods we need
func (m *MockConnectionPool) Stat(ctx context.Context, id string, groups []string) (int, error) {
	args := m.Called(ctx, id, groups)
	return args.Int(0), args.Error(1)
}

// MockPoolManager implements pool.Manager for testing
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
	args := m.Called(providers)
	return args.Error(0)
}

func (m *MockPoolManager) ClearPool() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockPoolManager) HasPool() bool {
	args := m.Called()
	return args.Bool(0)
}

func (m *MockPoolManager) GetMetrics() (pool.MetricsSnapshot, error) {
	args := m.Called()
	return args.Get(0).(pool.MetricsSnapshot), args.Error(1)
}

func TestValidateSegmentAvailability(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockPool := new(MockConnectionPool)
		mockManager := new(MockPoolManager)

		segments := []*metapb.SegmentData{
			{Id: "seg1"},
			{Id: "seg2"},
		}

		mockManager.On("GetPool").Return(mockPool, nil)
		mockPool.On("Stat", mock.Anything, "seg1", []string{}).Return(0, nil)
		mockPool.On("Stat", mock.Anything, "seg2", []string{}).Return(0, nil)

		err := ValidateSegmentAvailability(ctx, segments, mockManager, 1, 100, nil, time.Second)
		assert.NoError(t, err)

		mockManager.AssertExpectations(t)
		mockPool.AssertExpectations(t)
	})

	t.Run("failure", func(t *testing.T) {
		mockPool := new(MockConnectionPool)
		mockManager := new(MockPoolManager)

		segments := []*metapb.SegmentData{
			{Id: "seg1"},
		}

		mockManager.On("GetPool").Return(mockPool, nil)
		mockPool.On("Stat", mock.Anything, "seg1", []string{}).Return(0, fmt.Errorf("not found"))

		err := ValidateSegmentAvailability(ctx, segments, mockManager, 1, 100, nil, time.Second)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "unreachable")

		mockManager.AssertExpectations(t)
		mockPool.AssertExpectations(t)
	})
}

func TestValidateSegmentAvailabilityDetailed(t *testing.T) {
	ctx := context.Background()

	t.Run("partial failure", func(t *testing.T) {
		mockPool := new(MockConnectionPool)
		mockManager := new(MockPoolManager)

		segments := []*metapb.SegmentData{
			{Id: "seg1"},
			{Id: "seg2"},
		}

		mockManager.On("GetPool").Return(mockPool, nil)
		mockPool.On("Stat", mock.Anything, "seg1", []string{}).Return(0, nil)
		mockPool.On("Stat", mock.Anything, "seg2", []string{}).Return(0, fmt.Errorf("not found"))

		result, err := ValidateSegmentAvailabilityDetailed(ctx, segments, mockManager, 1, 100, nil, time.Second)
		assert.NoError(t, err)
		assert.Equal(t, 2, result.TotalChecked)
		assert.Equal(t, 1, result.MissingCount)
		assert.Contains(t, result.MissingIDs, "seg2")

		mockManager.AssertExpectations(t)
		mockPool.AssertExpectations(t)
	})
}

func TestSelectSegmentsForValidation(t *testing.T) {
	// Create 100 dummy segments
	segments := make([]*metapb.SegmentData, 100)
	for i := 0; i < 100; i++ {
		segments[i] = &metapb.SegmentData{Id: fmt.Sprintf("seg%d", i)}
	}

	t.Run("100 percent", func(t *testing.T) {
		selected := selectSegmentsForValidation(segments, 100)
		assert.Equal(t, 100, len(selected))
	})

	t.Run("10 percent", func(t *testing.T) {
		selected := selectSegmentsForValidation(segments, 10)
		// 10% of 100 = 10 segments
		assert.Equal(t, 10, len(selected))

		// Should include first 3
		assert.Equal(t, "seg0", selected[0].Id)
		assert.Equal(t, "seg1", selected[1].Id)
		assert.Equal(t, "seg2", selected[2].Id)

		// Should include last 2
		found98 := false
		found99 := false
		for _, s := range selected {
			if s.Id == "seg98" {
				found98 = true
			}
			if s.Id == "seg99" {
				found99 = true
			}
		}
		assert.True(t, found98, "Should include seg98")
		assert.True(t, found99, "Should include seg99")
	})

	t.Run("minimum 5", func(t *testing.T) {
		// 1% of 100 = 1 segment, but minimum is 5
		selected := selectSegmentsForValidation(segments, 1)
		assert.Equal(t, 5, len(selected))
	})
}