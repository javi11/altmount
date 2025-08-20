package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPIServer(t *testing.T) {
	// Create in-memory database for testing
	db, err := database.NewDB(database.Config{DatabasePath: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	// Create repositories
	queueRepo := database.NewRepository(db.Connection())
	healthRepo := database.NewHealthRepository(db.Connection())

	// Create shared mux and API server
	mux := http.NewServeMux()
	config := &Config{
		Enabled:  true,
		Prefix:   "/api",
		Username: "",
		Password: "",
	}
	NewServer(config, queueRepo, healthRepo, mux)

	t.Run("GET /api/queue/stats", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/queue/stats", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response APIResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)
		assert.True(t, response.Success)
		assert.NotNil(t, response.Data)
	})

	t.Run("GET /api/health/stats", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/health/stats", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response APIResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)
		assert.True(t, response.Success)
		assert.NotNil(t, response.Data)
	})

	t.Run("GET /api/system/health", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/system/health", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response APIResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)
		assert.True(t, response.Success)

		// Check that response data is a SystemHealthResponse
		healthData := response.Data.(map[string]interface{})
		assert.Contains(t, healthData, "status")
		assert.Contains(t, healthData, "components")
	})

	t.Run("GET /api/queue with pagination", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/queue?limit=10&offset=0", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

		var response APIResponse
		err := json.NewDecoder(w.Body).Decode(&response)
		assert.NoError(t, err)
		assert.True(t, response.Success)
		assert.NotNil(t, response.Meta)

		// Check pagination metadata
		meta := response.Meta
		assert.Equal(t, 10, meta.Limit)
		assert.Equal(t, 0, meta.Offset)
	})

	t.Run("GET non-existent endpoint returns 404", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/api/nonexistent", nil)
		w := httptest.NewRecorder()

		mux.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}