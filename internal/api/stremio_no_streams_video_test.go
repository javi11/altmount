package api

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func boolPtr(b bool) *bool { return &b }

func TestEmptyStreamsResponse_PlaceholderVideoEnabled(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
	}{
		{"nil config defaults to enabled", nil},
		{"explicitly enabled", &config.Config{Stremio: config.StremioConfig{ShowNoStreamsVideo: boolPtr(true)}}},
		{"unset defaults to enabled", &config.Config{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			s := &Server{}
			var body []byte
			app.Get("/:key", func(c *fiber.Ctx) error {
				if err := s.emptyStreamsResponse(c, tc.cfg, c.Params("key")); err != nil {
					return err
				}
				body = c.Response().Body()
				return nil
			})

			req := httptest.NewRequest("GET", "/abc123", nil)
			resp, err := app.Test(req)
			require.NoError(t, err)
			require.Equal(t, fiber.StatusOK, resp.StatusCode)

			var parsed struct {
				Streams []map[string]any `json:"streams"`
			}
			require.NoError(t, json.Unmarshal(body, &parsed))
			require.Len(t, parsed.Streams, 1)

			stream := parsed.Streams[0]
			assert.Equal(t, noStreamsVideoTitle, stream["title"])
			assert.Equal(t, "http://example.com/stremio/abc123/no-streams.mp4", stream["url"])
		})
	}
}

func TestEmptyStreamsResponse_PlaceholderVideoDisabled(t *testing.T) {
	app := fiber.New()
	s := &Server{}
	disabled := &config.Config{Stremio: config.StremioConfig{ShowNoStreamsVideo: boolPtr(false)}}

	var body []byte
	app.Get("/:key", func(c *fiber.Ctx) error {
		if err := s.emptyStreamsResponse(c, disabled, c.Params("key")); err != nil {
			return err
		}
		body = c.Response().Body()
		return nil
	})

	req := httptest.NewRequest("GET", "/abc123", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusOK, resp.StatusCode)
	assert.JSONEq(t, `{"streams": []}`, string(body))
}

func TestHandleStremioNoStreamsVideo_AuthAndDisabled(t *testing.T) {
	t.Run("rejects invalid key", func(t *testing.T) {
		app := fiber.New()
		enabled := true
		s := &Server{configManager: &mockConfigManager{cfg: &config.Config{
			Stremio: config.StremioConfig{Enabled: &enabled},
		}}}
		app.Get("/:key/no-streams.mp4", s.handleStremioNoStreamsVideo)

		req := httptest.NewRequest("GET", "/badkey/no-streams.mp4", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusUnauthorized, resp.StatusCode)
	})

	t.Run("404 when stremio disabled", func(t *testing.T) {
		app := fiber.New()
		s := &Server{configManager: &mockConfigManager{cfg: &config.Config{}}}
		app.Get("/:key/no-streams.mp4", s.handleStremioNoStreamsVideo)

		req := httptest.NewRequest("GET", "/somekey/no-streams.mp4", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusNotFound, resp.StatusCode)
	})
}

func TestServeNoStreamsVideo(t *testing.T) {
	setup := func() (*fiber.App, func()) {
		app := fiber.New()
		app.Get("/video.mp4", serveNoStreamsVideo)
		return app, func() {}
	}

	t.Run("full response", func(t *testing.T) {
		app, release := setup()
		defer release()

		req := httptest.NewRequest("GET", "/video.mp4", nil)
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, "video/mp4", resp.Header.Get("Content-Type"))
		assert.Equal(t, "bytes", resp.Header.Get("Accept-Ranges"))
		assert.Equal(t, len(noStreamsVideo), int(resp.ContentLength))
	})

	t.Run("range request returns partial content", func(t *testing.T) {
		app, release := setup()
		defer release()

		req := httptest.NewRequest("GET", "/video.mp4", nil)
		req.Header.Set("Range", "bytes=100-199")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusPartialContent, resp.StatusCode)
		assert.Equal(t, "bytes 100-199/"+fmt.Sprint(len(noStreamsVideo)), resp.Header.Get("Content-Range"))
		assert.Equal(t, 100, int(resp.ContentLength))
	})

	t.Run("open-ended range returns remainder", func(t *testing.T) {
		app, release := setup()
		defer release()

		req := httptest.NewRequest("GET", "/video.mp4", nil)
		req.Header.Set("Range", "bytes=0-")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusPartialContent, resp.StatusCode)
		assert.Equal(t, len(noStreamsVideo), int(resp.ContentLength))
	})

	t.Run("malformed range falls back to full body", func(t *testing.T) {
		app, release := setup()
		defer release()

		req := httptest.NewRequest("GET", "/video.mp4", nil)
		req.Header.Set("Range", "bytes=999999-")
		resp, err := app.Test(req)
		require.NoError(t, err)
		assert.Equal(t, fiber.StatusOK, resp.StatusCode)
		assert.Equal(t, len(noStreamsVideo), int(resp.ContentLength))
	})
}

func TestParseByteRange(t *testing.T) {
	size := 1000

	tests := []struct {
		name      string
		header    string
		wantStart int
		wantEnd   int
		wantOK    bool
	}{
		{"simple range", "bytes=0-99", 0, 99, true},
		{"mid range", "bytes=100-199", 100, 199, true},
		{"open ended", "bytes=500-", 500, 999, true},
		{"suffix last n", "bytes=-200", 800, 999, true},
		{"suffix larger than size", "bytes=-5000", 0, 999, true},
		{"end clamped to size", "bytes=900-5000", 900, 999, true},
		{"no header", "", 0, 0, false},
		{"wrong unit", "items=0-10", 0, 0, false},
		{"multi part unsupported", "bytes=0-10,20-30", 0, 0, false},
		{"missing dash", "bytes=010", 0, 0, false},
		{"start beyond size", "bytes=1000-", 0, 0, false},
		{"end before start", "bytes=500-400", 0, 0, false},
		{"non numeric", "bytes=a-b", 0, 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			start, end, ok := parseByteRange(tc.header, size)
			assert.Equal(t, tc.wantOK, ok)
			if ok {
				assert.Equal(t, tc.wantStart, start)
				assert.Equal(t, tc.wantEnd, end)
			}
		})
	}
}
