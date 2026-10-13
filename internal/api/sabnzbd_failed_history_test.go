package api

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/importer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSABnzbdHistory_FailedVerificationCannotImportAnotherJob(t *testing.T) {
	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "history.db")})
	require.NoError(t, err)
	defer db.Close()
	repo := database.NewRepository(db.Connection(), db.Dialect())
	s := &Server{queueRepo: repo, importerService: &importer.Service{}}
	root := t.TempDir()
	s.configManager = &mockConfigManager{cfg: &config.Config{
		MountPath: root,
		Import:    config.ImportConfig{ImportStrategy: config.ImportStrategySYMLINK, ImportDir: &root},
		SABnzbd:   config.SABnzbdConfig{CompleteDir: "/", Categories: []config.SABnzbdCategory{{Name: "tv", Dir: "tv"}}},
	}}
	ctx := context.Background()
	failedID, nextID, category := "failed-download", "next-download", "tv"
	failed := &database.ImportQueueItem{NzbPath: "Lucky.S01E04.nzb", DownloadID: &failedID, Category: &category, Status: database.QueueStatusFailed, Priority: database.QueuePriorityNormal}
	require.NoError(t, repo.AddToQueue(ctx, failed))
	require.NoError(t, repo.AddImportHistory(ctx, &database.ImportHistory{NzbID: &failed.ID, DownloadID: &failedID, NzbName: failed.NzbPath, FileName: "Lucky.mkv", VirtualPath: "/tv/Lucky/Lucky.mkv", Category: &category}))
	nextPath := "/tv/Sugar/Sugar.S02E07.mkv"
	require.NoError(t, os.MkdirAll(filepath.Join(root, "tv/Sugar"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "tv/Sugar/Sugar.S02E07.mkv"), []byte("media"), 0644))
	next := &database.ImportQueueItem{NzbPath: "Sugar.S02E07.nzb", DownloadID: &nextID, Category: &category, Status: database.QueueStatusCompleted, Priority: database.QueuePriorityNormal, StoragePath: &nextPath}
	require.NoError(t, repo.AddToQueue(ctx, next))
	require.NoError(t, db.Repository.AddStoragePath(ctx, next.ID, nextPath))
	app := fiber.New()
	app.Get("/history", s.handleSABnzbdHistory)
	for _, query := range []string{"", "?nzo_ids=failed-download,next-download", "?cat=tv&limit=1", "?nzo_ids=failed-download"} {
		t.Run(query, func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest("GET", "/history"+query, nil))
			require.NoError(t, err)
			defer resp.Body.Close()
			var result SABnzbdCompleteHistoryResponse
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
			seen := map[string]bool{}
			for _, slot := range result.History.Slots {
				assert.False(t, seen[slot.NzoID], "duplicate download ID")
				seen[slot.NzoID] = true
				if slot.NzoID == failedID {
					assert.Equal(t, "Failed", slot.Status)
					assert.Empty(t, slot.Storage, "failed job must not expose the import root")
					assert.Empty(t, slot.Path)
				} else {
					assert.Equal(t, nextID, slot.NzoID)
					assert.Equal(t, "Completed", slot.Status)
					assert.Equal(t, filepath.Join(root, "tv/Sugar/Sugar.S02E07.mkv"), slot.Storage)
				}
			}
			if query != "?cat=tv&limit=1" {
				assert.True(t, seen[failedID])
			}
			if query == "" {
				assert.Equal(t, 2, result.History.Noofslots)
				assert.Len(t, result.History.Slots, 2)
			}
		})
	}
}

func TestSABnzbdHistory_MissingStorageCannotExposeImportRoot(t *testing.T) {
	s, repo := newSABnzbdDeleteTestServer(t)
	root := t.TempDir()
	s.configManager = &mockConfigManager{cfg: &config.Config{MountPath: root, Import: config.ImportConfig{ImportStrategy: config.ImportStrategySYMLINK, ImportDir: &root}}}
	ctx := context.Background()
	item := &database.ImportQueueItem{NzbPath: "incomplete.nzb", Status: database.QueueStatusCompleted, Priority: database.QueuePriorityNormal}
	require.NoError(t, repo.AddToQueue(ctx, item))
	app := fiber.New()
	app.Get("/history", s.handleSABnzbdHistory)
	resp, err := app.Test(httptest.NewRequest("GET", "/history", nil))
	require.NoError(t, err)
	defer resp.Body.Close()
	var result SABnzbdCompleteHistoryResponse
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&result))
	require.Len(t, result.History.Slots, 1)
	assert.Equal(t, "Failed", result.History.Slots[0].Status)
	assert.Empty(t, result.History.Slots[0].Storage)
	assert.Empty(t, result.History.Slots[0].Path)
}
