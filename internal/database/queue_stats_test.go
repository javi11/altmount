package database

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Imports parked for a PAR2 repair have their own count; they used to be
// counted nowhere, so the queue page showed the failed count on that tab.
func TestGetQueueStatsCountsWaitingRepair(t *testing.T) {
	ctx := context.Background()
	db, err := NewDB(Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "stats.db")})
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })
	repo := NewRepository(db.Connection(), db.Dialect())
	queueRepo := NewQueueRepository(db.Connection(), db.Dialect())

	for i, status := range []QueueStatus{QueueStatusFailed, QueueStatusWaitingRepair, QueueStatusWaitingRepair, QueueStatusCompleted} {
		item := &ImportQueueItem{NzbPath: fmt.Sprintf("/nzbs/%d.nzb", i), Status: QueueStatusPending, Priority: QueuePriorityNormal}
		require.NoError(t, repo.AddToQueue(ctx, item))
		require.NoError(t, repo.UpdateQueueItemStatus(ctx, item.ID, status, nil))
	}

	stats, err := repo.GetQueueStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, stats.TotalWaitingRepair)
	assert.Equal(t, 1, stats.TotalFailed)
	assert.Equal(t, 1, stats.TotalCompleted)

	live, err := queueRepo.GetQueueStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 2, live.TotalWaitingRepair)
	assert.Equal(t, 1, live.TotalFailed)
}
