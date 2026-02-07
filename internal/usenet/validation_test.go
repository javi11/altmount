package usenet

import (
	"context"
	"fmt"
	"io"
	"math/rand"
	"testing"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v2"
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
func (m *MockPoolManager) ResetMetrics(_ context.Context) error {
	return nil
}

// MockConnectionPool
type MockConnectionPool struct {
	mock.Mock
	nntppool.UsenetConnectionPool
}

func (m *MockConnectionPool) Stat(ctx context.Context, id string, groups []string) (int, error) {
	args := m.Called(ctx, id, groups)
	return args.Int(0), args.Error(1)
}

func (m *MockConnectionPool) Body(ctx context.Context, id string, w io.Writer, groups []string) (int64, error) {
	args := m.Called(ctx, id, w, groups)
	return int64(args.Int(0)), args.Error(1)
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
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, []string{}).Run(func(args mock.Arguments) {
		w := args.Get(2).(io.Writer)
		// Write some non-zero data
		_, _ = w.Write([]byte{0x01, 0x02, 0x03})
	}).Return(3, ErrLimitReached).Once()

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
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, []string{}).Return(0, fmt.Errorf("article not found")).Once()

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

func TestValidateSegmentAvailabilityDetailed_Hybrid_AllZeros(t *testing.T) {
	// Setup
	mockPool := new(MockConnectionPool)
	mockManager := new(MockPoolManager)
	mockManager.On("GetPool").Return(mockPool, nil)

	segments := []*metapb.SegmentData{
		{Id: "seg1", SegmentSize: 1000},
	}

	// Test hybrid mode (verifyData = true)
	// Mock Body call to write zeros
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, []string{}).Run(func(args mock.Arguments) {
		w := args.Get(2).(io.Writer)
		// Write 16 zeros
		_, _ = w.Write(make([]byte, 16))
	}).Return(16, ErrLimitReached).Once()

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
	assert.Equal(t, 1, result.MissingCount, "Should flag zero-filled segment as missing")
	assert.Equal(t, 1, result.TotalChecked)

	mockPool.AssertExpectations(t)
	mockManager.AssertExpectations(t)
}

func TestValidateSegmentAvailabilityDetailed_Hybrid_PartialZeros(t *testing.T) {
	// Setup
	mockPool := new(MockConnectionPool)
	mockManager := new(MockPoolManager)
	mockManager.On("GetPool").Return(mockPool, nil)

	segments := []*metapb.SegmentData{
		{Id: "seg1", SegmentSize: 1000},
	}

	// Test hybrid mode (verifyData = true)
	// Mock Body call to write mixed data: 8 zeros then 1 non-zero
	mockPool.On("Body", mock.Anything, "seg1", mock.Anything, []string{}).Run(func(args mock.Arguments) {
		w := args.Get(2).(io.Writer)
		// Write 8 zeros then one 0x01
		data := make([]byte, 16)
		data[8] = 0x01
		_, _ = w.Write(data)
	}).Return(16, ErrLimitReached).Once()

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
	assert.Equal(t, 0, result.MissingCount, "Should NOT flag segment with partial data as missing")
	assert.Equal(t, 1, result.TotalChecked)

	mockPool.AssertExpectations(t)
	mockManager.AssertExpectations(t)
}

func TestSelectSegmentsForValidation(t *testing.T) {
	// Seed random for predictability in middle segments
	rand.Seed(1)

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

	t.Run("cap 1005", func(t *testing.T) {
		// Create 20,000 segments (10% = 2000)
		largeSegments := make([]*metapb.SegmentData, 20000)
		for i := 0; i < 20000; i++ {
			largeSegments[i] = &metapb.SegmentData{Id: fmt.Sprintf("seg%d", i)}
		}

		selected := selectSegmentsForValidation(largeSegments, 10)
		assert.Equal(t, 1005, len(selected), "Should be capped at 1005")
	})
}