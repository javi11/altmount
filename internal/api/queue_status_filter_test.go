package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kipsilabs/altmount/internal/database"
)

// The Queue page has a tab for every status a queue item can hold, including
// "Waiting Repair" (imports parked for a PAR2 repair). GET /queue must filter
// by each of them; it used to accept only pending/processing/completed/failed,
// so opening the Waiting Repair tab failed with "Invalid status filter".
func TestListQueueFiltersByEveryStatus(t *testing.T) {
	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: filepath.Join(t.TempDir(), "queue.db")})
	require.NoError(t, err)
	defer db.Close()
	repo := database.NewRepository(db.Connection(), db.Dialect())
	s := &Server{queueRepo: repo}

	ctx := context.Background()
	for _, status := range queueStatusFilters {
		item := &database.ImportQueueItem{NzbPath: string(status) + ".nzb", Status: status, Priority: database.QueuePriorityNormal}
		require.NoError(t, repo.AddToQueue(ctx, item))
		// AddToQueue may normalise the initial status; set it explicitly.
		require.NoError(t, repo.UpdateQueueItemStatus(ctx, item.ID, status, nil))
	}

	app := fiber.New()
	app.Get("/queue", s.handleListQueue)

	for _, status := range queueStatusFilters {
		t.Run(string(status), func(t *testing.T) {
			resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/queue?status="+string(status), nil))
			require.NoError(t, err)
			defer resp.Body.Close()
			require.Equal(t, http.StatusOK, resp.StatusCode)

			var body struct {
				Success bool                `json:"success"`
				Data    []QueueItemResponse `json:"data"`
			}
			require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
			require.True(t, body.Success)
			require.Len(t, body.Data, 1, "status filter must return only its own item")
			assert.Equal(t, status, body.Data[0].Status)
			assert.Equal(t, string(status)+".nzb", body.Data[0].NzbPath)
		})
	}

	t.Run("unknown status is rejected", func(t *testing.T) {
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/queue?status=bogus", nil))
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}
