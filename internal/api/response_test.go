package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/stretchr/testify/assert"
)

func TestRespondServiceUnavailable(t *testing.T) {
	app := fiber.New()

	app.Get("/test", func(c *fiber.Ctx) error {
		return RespondServiceUnavailable(c, "Service is initializing", "Please wait")
	})

	req := httptest.NewRequest("GET", "/test", nil)
	resp, err := app.Test(req)

	assert.NoError(t, err)
	assert.Equal(t, 503, resp.StatusCode)

	// Check if Retry-After header exists
	retryAfter := resp.Header.Get("Retry-After")
	assert.Equal(t, "10", retryAfter, "Retry-After header should be set to 10")

	var body map[string]interface{}
	err = json.NewDecoder(resp.Body).Decode(&body)
	assert.NoError(t, err)
	assert.False(t, body["success"].(bool))
}

func TestServerReadiness(t *testing.T) {
	s := &Server{}
	assert.False(t, s.IsReady(), "Server should not be ready by default")

	s.SetReady(true)
	assert.True(t, s.IsReady(), "Server should be ready after SetReady(true)")

	s.SetReady(false)
	assert.False(t, s.IsReady(), "Server should not be ready after SetReady(false)")
}
