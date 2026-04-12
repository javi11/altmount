package queue

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockProcessor implements ItemProcessor for testing
type mockProcessor struct{}

func (m *mockProcessor) ProcessItem(_ context.Context, _ *database.ImportQueueItem) (string, error) {
	return "", nil
}
func (m *mockProcessor) HandleSuccess(_ context.Context, _ *database.ImportQueueItem, _ string) error {
	return nil
}
func (m *mockProcessor) HandleFailure(_ context.Context, _ *database.ImportQueueItem, _ error) {}

// testConfigGetter returns a config with a reasonable queue processing interval
func testConfigGetter() *config.Config {
	return &config.Config{
		Import: config.ImportConfig{
			QueueProcessingIntervalSeconds: 1,
		},
	}
}

func newTestManager(workers int) *Manager {
	return NewManager(
		ManagerConfig{
			Workers:      workers,
			ConfigGetter: testConfigGetter,
		},
		nil, // repository not needed for resize tests
		&mockProcessor{},
		nil, // listener
	)
}

func TestResize_ScaleUp(t *testing.T) {
	m := newTestManager(2)
	ctx := context.Background()

	require.NoError(t, m.Start(ctx))
	defer func() { _ = m.Stop(ctx) }()

	assert.Equal(t, 2, m.workerCount)

	require.NoError(t, m.Resize(ctx, 4))

	assert.Equal(t, 4, m.workerCount)
	assert.Equal(t, 4, m.config.Workers)
	assert.Len(t, m.workerCancels, 4)
}

func TestResize_ScaleDown(t *testing.T) {
	m := newTestManager(4)
	ctx := context.Background()

	require.NoError(t, m.Start(ctx))
	defer func() { _ = m.Stop(ctx) }()

	assert.Equal(t, 4, m.workerCount)

	require.NoError(t, m.Resize(ctx, 2))

	// Wait briefly for cancelled workers to exit
	time.Sleep(100 * time.Millisecond)

	assert.Equal(t, 2, m.workerCount)
	assert.Equal(t, 2, m.config.Workers)
	assert.Len(t, m.workerCancels, 2)
}

func TestResize_SameCount(t *testing.T) {
	m := newTestManager(3)
	ctx := context.Background()

	require.NoError(t, m.Start(ctx))
	defer func() { _ = m.Stop(ctx) }()

	require.NoError(t, m.Resize(ctx, 3))

	assert.Equal(t, 3, m.workerCount)
}

func TestResize_WhenStopped(t *testing.T) {
	m := newTestManager(2)
	ctx := context.Background()

	// Resize without starting — should just update config
	require.NoError(t, m.Resize(ctx, 5))

	assert.Equal(t, 5, m.config.Workers)
	assert.Equal(t, 0, m.workerCount) // No workers running
	assert.False(t, m.running)
}

func TestResize_InvalidCount(t *testing.T) {
	m := newTestManager(2)
	ctx := context.Background()

	err := m.Resize(ctx, 0)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "worker count must be positive")

	err = m.Resize(ctx, -1)
	assert.Error(t, err)
}

func TestResize_ScaleDownWorkersStop(t *testing.T) {
	m := newTestManager(4)
	ctx := context.Background()

	require.NoError(t, m.Start(ctx))
	defer func() { _ = m.Stop(ctx) }()

	// Scale down from 4 to 1
	require.NoError(t, m.Resize(ctx, 1))

	// Wait for workers to stop (they exit on loopCtx.Done())
	time.Sleep(200 * time.Millisecond)

	assert.Equal(t, 1, m.workerCount)
	assert.Len(t, m.workerCancels, 1)
}

func TestResize_ScaleUpThenDown(t *testing.T) {
	m := newTestManager(2)
	ctx := context.Background()

	require.NoError(t, m.Start(ctx))
	defer func() { _ = m.Stop(ctx) }()

	// Scale up
	require.NoError(t, m.Resize(ctx, 6))
	assert.Equal(t, 6, m.workerCount)

	// Scale back down
	require.NoError(t, m.Resize(ctx, 3))
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 3, m.workerCount)
}
