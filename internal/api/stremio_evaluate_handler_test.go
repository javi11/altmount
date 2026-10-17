package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/internal/stremio"
)

func evaluateRelease(t *testing.T, body any) (int, map[string]any) {
	t.Helper()
	app := fiber.New()
	server := &Server{configManager: &mockConfigManager{cfg: config.DefaultConfig()}}
	app.Post("/api/stremio/scoring/evaluate", server.handleEvaluateStremioRelease)

	raw, _ := json.Marshal(body)
	req := httptest.NewRequest("POST", "/api/stremio/scoring/evaluate", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	var out map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return resp.StatusCode, out
}

// The config page's single-title tester scores with the same scorer that
// ranks Stremio streams, including the language bonus.
func TestEvaluateStremioRelease(t *testing.T) {
	status, out := evaluateRelease(t, EvaluateReleaseRequest{
		Title: "Knight.And.Day.2010.1080p.DSNP.WEB-DL.DDP5.1.H.264-GRP",
		Scoring: &stremio.StreamScoringConfig{
			PreferredLanguages: []string{"English", "Original Audio"},
			CustomFormats: []stremio.TrashCustomFormat{
				{ID: "webdl", Name: "1080p WEB-DL", Pattern: `\b1080p\b.*\b(web[ ._-]?dl|webrip)\b`, Score: 120, Enabled: true},
			},
		},
	})
	require.Equal(t, 200, status)
	data := out["data"].(map[string]any)
	assert.Equal(t, float64(570), data["score"])
	assert.Equal(t, float64(450), data["language_bonus"])
	assert.Equal(t, []any{stremio.OriginalAudio}, data["matched_languages"])
	assert.Equal(t, []any{map[string]any{"id": "webdl", "name": "1080p WEB-DL", "score": float64(120)}}, data["formats"])
	assert.Equal(t, false, data["excluded"])
}

func TestEvaluateStremioReleaseRequiresTitle(t *testing.T) {
	status, _ := evaluateRelease(t, EvaluateReleaseRequest{Title: "  "})
	assert.Equal(t, 400, status)
}
