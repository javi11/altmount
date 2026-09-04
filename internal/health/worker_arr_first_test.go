package health

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golift.io/starr"

	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// recordingPar2Enqueuer is a Par2RepairEnqueuer fake that records enqueues.
type recordingPar2Enqueuer struct {
	calls []string
}

func (r *recordingPar2Enqueuer) Enqueue(_ context.Context, filePath string, _ string) {
	r.calls = append(r.calls, filePath)
}

// withPar2Repair enables the PAR2 repair feature (arr_first stays at its
// default: on).
func withPar2Repair(cfg *config.Config) {
	enabled := true
	cfg.Par2Repair.Enabled = &enabled
}

// TestPar2FallbackAfterArrRepair covers the ARR-first ordering in the health
// worker's corrupted flow: the ARR rescan runs first exactly as before, and a
// PAR2 repair is enqueued only when nothing is found in the ARRs (path miss,
// no instance configured) or ARR repair is disabled.
func TestPar2FallbackAfterArrRepair(t *testing.T) {
	ctx := context.Background()
	filePath := "movies/movie.mkv"

	corruptedEvent := HealthEvent{
		Type:     EventTypeFileCorrupted,
		FilePath: filePath,
		Status:   database.HealthStatusCorrupted,
	}
	// A file on its last health-check retry, so prepareUpdateForResult takes
	// the repair-trigger branch.
	exhaustedFH := func(env *repairTestEnv) *database.FileHealth {
		return &database.FileHealth{
			FilePath:   filePath,
			Status:     database.HealthStatusPending,
			RetryCount: env.hw.configGetter().GetMaxRetries() - 1,
		}
	}
	runRepairBranch := func(t *testing.T, env *repairTestEnv, par2 *recordingPar2Enqueuer) *database.HealthStatusUpdate {
		t.Helper()
		env.hw.SetPar2RepairEnqueuer(par2)
		require.NoError(t, env.metadataService.WriteFileMetadata(filePath,
			validSegmentMeta(env.metadataService, 1024)))
		update, sideEffect := env.hw.prepareUpdateForResult(ctx, exhaustedFH(env), corruptedEvent)
		require.NoError(t, sideEffect())
		return update
	}

	t.Run("arr accepts the rescan: no PAR2 fallback", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), nil, withPar2Repair)
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusRepairTriggered, update.Status)
		assert.Len(t, env.mockARRs.calls, 1, "ARR repair runs first, as before")
		assert.Empty(t, par2.calls, "the ARR took the file; PAR2 must stay out")
	})

	t.Run("nothing found in the arrs: PAR2 fallback enqueued", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), arrs.ErrPathMatchFailed, withPar2Repair)
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusCorrupted, update.Status)
		assert.Equal(t, []string{filePath}, par2.calls, "PAR2 is second in line")
	})

	t.Run("arr temporarily unreachable: deferred, no PAR2 fallback", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), &starr.ReqError{Code: 503}, withPar2Repair)
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusRepairTriggered, update.Status,
			"an unreachable ARR defers the repair instead of condemning the file")
		assert.Empty(t, par2.calls, "the ARR still gets its chance on the next cycle")
	})

	t.Run("arr repair disabled: PAR2 is the only option", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), nil, withPar2Repair, func(cfg *config.Config) {
			repairEnabled := false
			cfg.Health.Repair.Enabled = &repairEnabled
		})
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusCorrupted, update.Status)
		assert.Empty(t, env.mockARRs.calls, "ARR repair is disabled")
		assert.Equal(t, []string{filePath}, par2.calls)
	})

	t.Run("arr_first disabled: no PAR2 fallback", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), arrs.ErrPathMatchFailed, withPar2Repair, func(cfg *config.Config) {
			arrFirst := false
			cfg.Par2Repair.ArrFirst = &arrFirst
		})
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusCorrupted, update.Status)
		assert.Empty(t, par2.calls, "fallback switched off keeps the old ARR-only flow")
	})

	t.Run("par2 repair disabled: no fallback", func(t *testing.T) {
		env := newRepairTestEnv(t, t.TempDir(), arrs.ErrPathMatchFailed)
		par2 := &recordingPar2Enqueuer{}

		update := runRepairBranch(t, env, par2)

		assert.Equal(t, database.HealthStatusCorrupted, update.Status)
		assert.Empty(t, par2.calls)
	})

	t.Run("already repair_triggered with repair disabled: no fallback", func(t *testing.T) {
		// The file's metadata may already sit in the safety folder from the
		// earlier successful ARR trigger, which a PAR2 repair cannot read.
		env := newRepairTestEnv(t, t.TempDir(), nil, withPar2Repair, func(cfg *config.Config) {
			repairEnabled := false
			cfg.Health.Repair.Enabled = &repairEnabled
		})
		par2 := &recordingPar2Enqueuer{}
		env.hw.SetPar2RepairEnqueuer(par2)

		fh := &database.FileHealth{FilePath: filePath, Status: database.HealthStatusRepairTriggered}
		update, sideEffect := env.hw.prepareUpdateForResult(ctx, fh, corruptedEvent)
		require.NoError(t, sideEffect())

		assert.Equal(t, database.HealthStatusCorrupted, update.Status)
		assert.Empty(t, par2.calls)
	})
}
