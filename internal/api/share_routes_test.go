package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/sharenet"
)

func TestShareRoutes_MethodAndRateLimit(t *testing.T) {
	app := fiber.New()
	store := sharenet.NewReleaseStore(t.TempDir())
	cfg := &config.Config{Share: config.ShareConfig{RateLimitPerMinute: 3}}
	s := &Server{shareStore: store, configManager: &mockConfigManager{cfg: cfg}}
	s.registerShareRoutes(app.Group("/api"))

	hash := strings.Repeat("a", 64)

	// POST is rejected — the share routes are GET-only.
	resp, err := app.Test(httptest.NewRequest(http.MethodPost, "/api/share/manifest/"+hash, nil))
	if err != nil {
		t.Fatalf("POST request: %v", err)
	}
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("POST: got %d; want 405", resp.StatusCode)
	}

	// The 4th GET within the window trips the per-IP limit (Max=3).
	var last int
	for i := 0; i < 4; i++ {
		r, err := app.Test(httptest.NewRequest(http.MethodGet, "/api/share/manifest/"+hash, nil))
		if err != nil {
			t.Fatalf("GET %d: %v", i, err)
		}
		last = r.StatusCode
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("4th GET: got %d; want 429", last)
	}
}
