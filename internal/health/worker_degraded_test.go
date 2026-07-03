package health

import (
	"context"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/mediaprobe"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareUpdateForResultDegraded verifies the worker's decision table for
// classified events: degraded verdicts skip repair entirely, while fatal and
// unclassified events follow the pre-existing retry/repair path.
func TestPrepareUpdateForResultDegraded(t *testing.T) {
	tempDir := t.TempDir()
	env := newRepairTestEnv(t, tempDir, nil)

	filePath := "/movies/movie.mp4"
	meta := validSegmentMeta(env.metadataService, 1024)
	require.NoError(t, env.metadataService.WriteFileMetadata(filePath, meta))

	now := time.Now().UTC()
	baseFH := database.FileHealth{
		FilePath:  filePath,
		Status:    database.HealthStatusPending,
		CreatedAt: now,
	}
	corruptedEvent := func(cls *mediaprobe.Classification) HealthEvent {
		return HealthEvent{
			Type:           EventTypeFileCorrupted,
			FilePath:       filePath,
			Status:         database.HealthStatusCorrupted,
			Classification: cls,
		}
	}

	t.Run("degraded verdict skips repair", func(t *testing.T) {
		fh := baseFH
		fh.RetryCount = 99 // even with retries exhausted, degraded wins
		update, sideEffect := env.hw.prepareUpdateForResult(context.Background(), &fh,
			corruptedEvent(&mediaprobe.Classification{
				Verdict: mediaprobe.VerdictDegraded,
				Reason:  "missing ranges only intersect media payload",
			}))

		assert.Equal(t, database.UpdateTypeDegraded, update.Type)
		assert.Equal(t, database.HealthStatusDegraded, update.Status)
		assert.False(t, update.ScheduledCheckAt.IsZero(), "degraded records stay on the re-check schedule")

		require.NoError(t, sideEffect())
		assert.Empty(t, env.mockARRs.calls, "degraded must not trigger an ARR rescan")

		got, err := env.metadataService.ReadFileMetadata(filePath)
		require.NoError(t, err)
		assert.Equal(t, metapb.FileStatus_FILE_STATUS_DEGRADED, got.Status)
	})

	t.Run("in-flight repair wins over degraded", func(t *testing.T) {
		fh := baseFH
		fh.Status = database.HealthStatusRepairTriggered
		update, _ := env.hw.prepareUpdateForResult(context.Background(), &fh,
			corruptedEvent(&mediaprobe.Classification{Verdict: mediaprobe.VerdictDegraded}))
		assert.NotEqual(t, database.UpdateTypeDegraded, update.Type,
			"an already-triggered repair must not be downgraded")
	})

	t.Run("fatal verdict follows normal retry path", func(t *testing.T) {
		fh := baseFH
		update, _ := env.hw.prepareUpdateForResult(context.Background(), &fh,
			corruptedEvent(&mediaprobe.Classification{Verdict: mediaprobe.VerdictFatal}))
		assert.Equal(t, database.UpdateTypeRetry, update.Type)
		assert.Equal(t, database.HealthStatusPending, update.Status)
	})

	t.Run("unknown verdict follows normal retry path", func(t *testing.T) {
		fh := baseFH
		update, _ := env.hw.prepareUpdateForResult(context.Background(), &fh,
			corruptedEvent(&mediaprobe.Classification{Verdict: mediaprobe.VerdictUnknown}))
		assert.Equal(t, database.UpdateTypeRetry, update.Type)
	})

	t.Run("no classification follows normal retry path", func(t *testing.T) {
		fh := baseFH
		update, _ := env.hw.prepareUpdateForResult(context.Background(), &fh, corruptedEvent(nil))
		assert.Equal(t, database.UpdateTypeRetry, update.Type)
	})
}
