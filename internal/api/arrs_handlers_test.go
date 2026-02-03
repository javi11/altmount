package api

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/arrs"
	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
)

type mockConfigManager struct {
	cfg *config.Config
}

func (m *mockConfigManager) GetConfig() *config.Config {
	return m.cfg
}

func (m *mockConfigManager) UpdateConfig(cfg *config.Config) error {
	m.cfg = cfg
	return nil
}

func (m *mockConfigManager) ReloadConfig() error {
	return nil
}

func (m *mockConfigManager) ValidateConfig(cfg *config.Config) error {
	return nil
}

func (m *mockConfigManager) ValidateConfigUpdate(cfg *config.Config) error {
	return nil
}

func (m *mockConfigManager) OnConfigChange(callback config.ChangeCallback) {
}

func (m *mockConfigManager) SaveConfig() error {
	return nil
}

func (m *mockConfigManager) NeedsLibrarySync() bool {
	return false
}

func (m *mockConfigManager) GetPreviousMountPath() string {
	return ""
}

func (m *mockConfigManager) ClearLibrarySyncFlag() {
}

func TestHandleArrsWebhook_EpisodeFileDelete(t *testing.T) {
	app := fiber.New()
	
	keyOverride := "12345678901234567890123456789012" // 32 chars
	cfg := &config.Config{
		API: config.APIConfig{
			KeyOverride: keyOverride,
		},
	}
	
	server := &Server{
		configManager: &mockConfigManager{cfg: cfg},
		arrsService:   &arrs.Service{}, // non-nil
	}
	
	app.Post("/api/arrs/webhook", server.handleArrsWebhook)
	
	payload := map[string]interface{}{
		"eventType": "EpisodeFileDelete",
		"episodeFile": map[string]string{
			"path": "/some/path/episode.mkv",
		},
	}
	body, _ := json.Marshal(payload)
	
	req := httptest.NewRequest("POST", "/api/arrs/webhook?apikey="+keyOverride, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "Ignored", result["message"])
}

func TestHandleArrsWebhook_MovieFileDelete(t *testing.T) {
	app := fiber.New()
	
	keyOverride := "12345678901234567890123456789012" // 32 chars
	cfg := &config.Config{
		API: config.APIConfig{
			KeyOverride: keyOverride,
		},
	}
	
	server := &Server{
		configManager: &mockConfigManager{cfg: cfg},
		arrsService:   &arrs.Service{}, // non-nil
	}
	
	app.Post("/api/arrs/webhook", server.handleArrsWebhook)
	
	payload := map[string]interface{}{
		"eventType": "MovieFileDelete",
		"movie": map[string]string{
			"folderPath": "/some/path/movie",
		},
	}
	body, _ := json.Marshal(payload)
	
	req := httptest.NewRequest("POST", "/api/arrs/webhook?apikey="+keyOverride, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	
	resp, err := app.Test(req)
	assert.NoError(t, err)
	assert.Equal(t, 200, resp.StatusCode)
	
	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	
	assert.Equal(t, true, result["success"])
	assert.Equal(t, "Ignored", result["message"])
}

