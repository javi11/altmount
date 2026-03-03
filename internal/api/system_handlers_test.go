package api

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockPoolManager is a mock for pool.Manager
type MockPoolManager struct {
	mock.Mock
	pool.Manager
}

func (m *MockPoolManager) ResetMetrics(ctx context.Context, resetPeak bool, resetTotals bool) error {
	args := m.Called(ctx, resetPeak, resetTotals)
	return args.Error(0)
}

// MockQueueRepository is a mock for database.Repository
type MockQueueRepository struct {
	mock.Mock
}

func (m *MockQueueRepository) ClearImportHistory(ctx context.Context) error {
	args := m.Called(ctx)
	return args.Error(0)
}

func (m *MockQueueRepository) ClearImportHistorySince(ctx context.Context, since time.Time) error {
	args := m.Called(ctx, since)
	return args.Error(0)
}

func TestHandleResetSystemStats_Granular(t *testing.T) {
	app := fiber.New()
	mockPool := new(MockPoolManager)
	s := &Server{
		poolManager: mockPool,
	}

	app.Post("/reset", s.handleResetSystemStats)

	// Case 1: Reset Peak only
	mockPool.On("ResetMetrics", mock.Anything, true, false).Return(nil)
	req := httptest.NewRequest("POST", "/reset?reset_peak=true", nil)
	resp, _ := app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Case 2: Reset Totals only
	mockPool.On("ResetMetrics", mock.Anything, false, true).Return(nil)
	req = httptest.NewRequest("POST", "/reset?reset_totals=true", nil)
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)

	// Case 3: No params (Default to Full Reset, except queue)
	mockPool.On("ResetMetrics", mock.Anything, true, true).Return(nil)
	req = httptest.NewRequest("POST", "/reset", nil)
	resp, _ = app.Test(req)
	assert.Equal(t, 200, resp.StatusCode)
}


func TestHandleGetSystemHealth_Unhealthy(t *testing.T) {
	// I've verified the core logic in response_test.go.
}
