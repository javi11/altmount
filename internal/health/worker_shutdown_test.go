package health

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStopDuringActiveCycle verifies that Stop returns while a health check cycle is
// still draining. Stop used to hold hw.mu for its whole body, including wg.Wait(), while
// the finishing cycle needed that same lock to clear its running flag.
func TestStopDuringActiveCycle(t *testing.T) {
	tempDir := t.TempDir()
	bp := &blockingPoolManager{entered: make(chan struct{}), release: make(chan struct{})}
	env := newRepairTestEnvWithPool(t, tempDir, nil, bp, func(cfg *config.Config) {
		cfg.Health.CheckIntervalSeconds = 1
	})

	ctx := context.Background()
	filePath := "series/show.s02e01.mkv"

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))
	insertFileHealth(t, env.db, filePath, "/media/library/show.s02e01.mkv", 0, 3)

	require.NoError(t, env.hw.Start(ctx))

	select {
	case <-bp.entered:
	case <-time.After(10 * time.Second):
		close(bp.release)
		t.Fatal("health check cycle never started")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- env.hw.Stop(ctx) }()

	require.Eventually(t, func() bool {
		return env.hw.GetStats().Status == WorkerStatusStopping
	}, 2*time.Second, 5*time.Millisecond, "Stop never reached its wait")

	close(bp.release)

	select {
	case err := <-stopped:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Stop deadlocked while a health check cycle was draining")
	}

	assert.False(t, env.hw.IsRunning())
	assert.Equal(t, WorkerStatusStopped, env.hw.GetStats().Status)
}

// TestStartStopRestart verifies a worker can be restarted, which the config watcher does
// whenever health checking is toggled: the second Stop used to close an already closed
// channel because Start never re-armed it.
func TestStartStopRestart(t *testing.T) {
	env := newRepairTestEnv(t, t.TempDir(), nil)
	ctx := context.Background()

	for range 2 {
		require.NoError(t, env.hw.Start(ctx))
		assert.True(t, env.hw.IsRunning())
		require.NoError(t, env.hw.Stop(ctx))
		assert.False(t, env.hw.IsRunning())
	}
}
