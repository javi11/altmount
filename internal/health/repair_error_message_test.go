package health

import (
	"context"
	"errors"
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPrepareRepairNotificationUpdate_SuccessPreservesOriginalError guards
// against regressing to nulling last_error: a successful ARR re-trigger during
// a repair-retry cycle must not erase the original health-check diagnosis,
// since prepareRepairNotificationUpdate never sets ErrorMessage itself on the
// success path — it must default to fh.LastError up front.
func TestPrepareRepairNotificationUpdate_SuccessPreservesOriginalError(t *testing.T) {
	env := newRepairTestEnv(t, t.TempDir(), nil) // mockARRs.returnErr = nil → success

	original := "3 of 55 checked segments are missing from your Usenet provider"
	fh := &database.FileHealth{
		FilePath:         "complete/movie.mkv",
		Status:           database.HealthStatusRepairTriggered,
		LastError:        &original,
		RepairRetryCount: 0,
		MaxRepairRetries: 3,
	}

	update, sideEffect := env.hw.prepareRepairNotificationUpdate(context.Background(), fh)
	require.NotNil(t, sideEffect)
	require.NoError(t, sideEffect())

	require.NotNil(t, update.ErrorMessage)
	assert.Equal(t, original, *update.ErrorMessage)
}

// TestPrepareRepairNotificationUpdate_FailureCombinesErrors verifies a failed
// re-trigger appends the repair failure onto the original diagnosis instead
// of replacing it, so the UI still shows why the file was flagged corrupted.
func TestPrepareRepairNotificationUpdate_FailureCombinesErrors(t *testing.T) {
	env := newRepairTestEnv(t, t.TempDir(), errors.New("arr unreachable (test)"))

	original := "3 of 55 checked segments are missing from your Usenet provider"
	fh := &database.FileHealth{
		FilePath:         "complete/movie.mkv",
		Status:           database.HealthStatusRepairTriggered,
		LastError:        &original,
		RepairRetryCount: 0,
		MaxRepairRetries: 3,
	}

	update, sideEffect := env.hw.prepareRepairNotificationUpdate(context.Background(), fh)
	require.NotNil(t, sideEffect)
	require.NoError(t, sideEffect())

	require.NotNil(t, update.ErrorMessage)
	assert.Contains(t, *update.ErrorMessage, original)
	assert.Contains(t, *update.ErrorMessage, "repair failed")
	assert.Contains(t, *update.ErrorMessage, "arr unreachable (test)")
}

// TestPrepareRepairNotificationUpdate_RetriesExhaustedPreservesOriginalError
// covers the give-up branch: it must not null out the diagnosis either.
func TestPrepareRepairNotificationUpdate_RetriesExhaustedPreservesOriginalError(t *testing.T) {
	env := newRepairTestEnv(t, t.TempDir(), nil)

	original := "3 of 55 checked segments are missing from your Usenet provider"
	fh := &database.FileHealth{
		FilePath:         "complete/movie.mkv",
		Status:           database.HealthStatusRepairTriggered,
		LastError:        &original,
		RepairRetryCount: 3,
		MaxRepairRetries: 3,
	}

	update, _ := env.hw.prepareRepairNotificationUpdate(context.Background(), fh)
	require.NotNil(t, update.ErrorMessage)
	assert.Equal(t, original, *update.ErrorMessage)
}
