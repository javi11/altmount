package importer

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/importer/postprocessor"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleFailure_RollsBackHistoryBeforeRetry(t *testing.T) {
	for _, kind := range []string{"content", "inconclusive", "repair", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "import.db")})
			require.NoError(t, err)
			defer db.Close()
			cfg := &config.Config{}
			getter := func() *config.Config { return cfg }
			svc := &Service{database: db, configGetter: getter, log: slog.Default(), postProcessor: postprocessor.NewCoordinator(postprocessor.Config{ConfigGetter: getter})}
			repo := database.NewRepository(db.Connection(), db.Dialect())
			ctx := context.Background()
			download := "failed"
			item := &database.ImportQueueItem{NzbPath: "bad.nzb", DownloadID: &download, Status: database.QueueStatusProcessing, Priority: database.QueuePriorityNormal}
			require.NoError(t, db.Repository.AddToQueue(ctx, item))
			good := &database.ImportQueueItem{NzbPath: "good.nzb", Status: database.QueueStatusCompleted, Priority: database.QueuePriorityNormal}
			require.NoError(t, db.Repository.AddToQueue(ctx, good))
			goodID := good.ID
			require.NoError(t, repo.AddImportHistory(ctx, &database.ImportHistory{NzbID: &goodID, NzbName: "good.nzb", FileName: "good.mkv", VirtualPath: "/same/good.mkv"}))
			record := func(name string) {
				require.NoError(t, svc.AddImportHistory(ctx, &database.ImportHistory{NzbID: &item.ID, DownloadID: &download, NzbName: item.NzbPath, FileName: name, VirtualPath: "/same/" + name}))
			}
			record("bad.mkv")
			record("bad.srt")
			enabled := true
			cfg.Import.VerifyContent = &enabled
			svc.contentVerifyFS = &fakeContentOpener{data: make([]byte, 512)}
			failure := svc.verifyWrittenContent(ctx, []string{"bad.mkv"})
			require.Error(t, failure)
			switch kind {
			case "inconclusive":
				failure = fmt.Errorf("probe unavailable: %w", ErrContentProbeInconclusive)
			case "repair":
				failure = ErrDeferredForRepair
			case "cancelled":
				failure = context.Canceled
			}
			failureCtx := ctx
			if kind == "cancelled" {
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				failureCtx = cancelled
			}
			svc.HandleFailure(failureCtx, item, failure)
			var count int
			require.NoError(t, db.Connection().QueryRow("SELECT COUNT(*) FROM import_history WHERE nzb_id = ?", item.ID).Scan(&count))
			assert.Zero(t, count, "all files from the failed attempt must be rolled back")
			require.NoError(t, db.Connection().QueryRow("SELECT COUNT(*) FROM import_history WHERE nzb_id = ?", goodID).Scan(&count))
			assert.Equal(t, 1, count, "successful sibling history must survive")
			// A retry produces fresh history and becomes visible only on completion.
			require.NoError(t, db.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusPending, nil))
			record("retry.mkv")
			rows, err := repo.ListSABnzbdHistory(ctx, "", 100, 0)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			assert.Equal(t, goodID, rows[0].ID)
			require.NoError(t, db.Repository.UpdateQueueItemStatus(ctx, item.ID, database.QueueStatusCompleted, nil))
			rows, err = repo.ListSABnzbdHistory(ctx, "", 100, 0)
			require.NoError(t, err)
			assert.Len(t, rows, 2)
		})
	}
}
