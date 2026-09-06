package health

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/pool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// blockingPoolManager parks the segment-availability sweep so a test can observe and act
// on a cycle while its files are still in 'checking'.
type blockingPoolManager struct {
	mockPoolManager
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (m *blockingPoolManager) GetPool() (pool.NntpClient, error) {
	m.once.Do(func() { close(m.entered) })
	<-m.release
	return nil, errors.New("no pool available (test mock)")
}

// TestRunHealthCheckCycle_CancelDuringBatchCheck verifies that a file being checked by the
// scheduled worker cycle — not just by a manual check-now — is cancellable: it is registered
// as an active check, and cancelling it suppresses that file's result write and repair
// side effects instead of letting the cycle finish it.
func TestRunHealthCheckCycle_CancelDuringBatchCheck(t *testing.T) {
	tempDir := t.TempDir()
	bp := &blockingPoolManager{entered: make(chan struct{}), release: make(chan struct{})}
	env := newRepairTestEnvWithPool(t, tempDir, nil, bp)

	ctx := context.Background()
	filePath := "series/show.s01e01.mkv"
	libraryPath := "/media/library/show.s01e01.mkv"
	maxRetries := 3

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	// Last retry: without cancellation this cycle would trigger ARR repair.
	insertFileHealth(t, env.db, filePath, libraryPath, maxRetries-1, maxRetries)

	done := make(chan error, 1)
	go func() { done <- env.hw.runHealthCheckCycle(ctx) }()

	<-bp.entered // the sweep is in flight; the record is 'checking'

	require.True(t, env.hw.IsCheckActive(filePath),
		"a file being checked by the worker cycle must be reported as an active check")
	require.NoError(t, env.hw.CancelHealthCheck(ctx, filePath))

	close(bp.release)
	require.NoError(t, <-done)

	env.mockARRs.mu.Lock()
	calls := len(env.mockARRs.calls)
	env.mockARRs.mu.Unlock()
	assert.Zero(t, calls, "a cancelled check must not trigger repair side effects")

	fh, err := env.healthRepo.GetFileHealth(ctx, filePath)
	require.NoError(t, err)
	require.NotNil(t, fh)
	assert.Equal(t, database.HealthStatusPending, fh.Status,
		"a cancelled check must leave the record pending, not write its result")

	assert.False(t, env.hw.IsCheckActive(filePath),
		"the cycle must release its active-check registrations when it finishes")
}

// TestRunHealthCheckCycle_ReleasesActiveChecks verifies an uncancelled cycle leaves no
// stale active-check registrations behind, which would block later cancel requests.
func TestRunHealthCheckCycle_ReleasesActiveChecks(t *testing.T) {
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil)

	ctx := context.Background()
	filePath := "series/show.s01e02.mkv"

	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))
	insertFileHealth(t, env.db, filePath, "/media/library/show.s01e02.mkv", 0, 3)

	require.NoError(t, env.hw.runHealthCheckCycle(ctx))

	assert.False(t, env.hw.IsCheckActive(filePath))
}
