package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/updater"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsDockerAvailable(t *testing.T) {
	// Ensure isDockerAvailable does not panic and returns a bool.
	available := isDockerAvailable()
	t.Logf("Docker available in this environment: %v", available)
	assert.NotPanics(t, func() { isDockerAvailable() })
}

// fakeUpdater is a test double implementing updater.Updater.
type fakeUpdater struct {
	canSelfUpdate bool
	applyErr      error
	applyCalls    atomic.Int32
	lastChannel   atomic.Value // string
}

func (f *fakeUpdater) CanSelfUpdate() bool { return f.canSelfUpdate }

func (f *fakeUpdater) ApplyBinaryUpdate(_ context.Context, channel string) error {
	f.applyCalls.Add(1)
	f.lastChannel.Store(channel)
	return f.applyErr
}

func TestHandleGetUpdateStatus_PopulatesBinaryField(t *testing.T) {
	app := fiber.New()
	loginRequired := false
	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{
			Auth: config.AuthConfig{LoginRequired: &loginRequired},
		}},
		updater: &fakeUpdater{canSelfUpdate: true},
	}
	app.Get("/status", s.handleGetUpdateStatus)

	req := httptest.NewRequest("GET", "/status?channel=latest", nil)
	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		Data UpdateStatusResponse `json:"data"`
	}
	require.NoError(t, json.Unmarshal(body, &parsed))
	assert.True(t, parsed.Data.BinaryUpdateAvailable, "binary_update_available should reflect updater.CanSelfUpdate")
}

// postApplyUpdate posts a body to the apply handler and returns the response.
func postApplyUpdate(t *testing.T, s *Server, body any) (status int, decoded map[string]any) {
	t.Helper()
	app := fiber.New()
	app.Post("/apply", s.handleApplyUpdate)

	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(body))
	req := httptest.NewRequest("POST", "/apply", &buf)
	req.Header.Set("Content-Type", "application/json")

	resp, err := app.Test(req, -1)
	require.NoError(t, err)
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(raw, &decoded)
	return resp.StatusCode, decoded
}

func TestHandleApplyUpdate_NoPathAvailable(t *testing.T) {
	// Skip if running inside a container (the Docker branch would be
	// evaluated instead of the "no path" branch).
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running inside docker; /.dockerenv present")
	}

	loginRequired := false
	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{
			Auth: config.AuthConfig{LoginRequired: &loginRequired},
		}},
		updater: &fakeUpdater{canSelfUpdate: false},
	}

	status, decoded := postApplyUpdate(t, s, map[string]string{"channel": "latest"})
	assert.Equal(t, 400, status)
	assert.Equal(t, false, decoded["success"])
}

func TestHandleApplyUpdate_BinaryBranch(t *testing.T) {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running inside docker; /.dockerenv present")
	}

	loginRequired := false
	// Make ApplyBinaryUpdate return an error so performRestart is not
	// invoked in the background goroutine — performRestart would syscall.Exec
	// the test binary and kill the entire test run.
	fake := &fakeUpdater{canSelfUpdate: true, applyErr: errTestApply}
	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{
			Auth: config.AuthConfig{LoginRequired: &loginRequired},
		}},
		updater: fake,
	}

	status, decoded := postApplyUpdate(t, s, map[string]string{"channel": "dev"})
	assert.Equal(t, 200, status)
	assert.Equal(t, true, decoded["success"])

	// Wait for the background goroutine to observe the fake updater call.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fake.applyCalls.Load() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.GreaterOrEqual(t, int(fake.applyCalls.Load()), 1, "ApplyBinaryUpdate should have been invoked")
	if v := fake.lastChannel.Load(); v != nil {
		assert.Equal(t, "dev", v.(string))
	}
}

var errTestApply = errors.New("test apply failure")

func TestHandleApplyUpdate_InvalidChannel(t *testing.T) {
	loginRequired := false
	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{
			Auth: config.AuthConfig{LoginRequired: &loginRequired},
		}},
		updater: &fakeUpdater{canSelfUpdate: true},
	}

	status, _ := postApplyUpdate(t, s, map[string]string{"channel": "banana"})
	assert.Equal(t, 400, status)
}

func TestHandleApplyUpdate_DockerBranchRespected(t *testing.T) {
	// This test verifies the decision logic: when the fake updater says it
	// cannot self-update AND we're not in a container, we must return 400.
	// It is a sibling to TestHandleApplyUpdate_NoPathAvailable to make
	// explicit that s.updater is consulted.
	if _, err := os.Stat("/.dockerenv"); err == nil {
		t.Skip("running inside docker; /.dockerenv present")
	}
	loginRequired := false
	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{
			Auth: config.AuthConfig{LoginRequired: &loginRequired},
		}},
		updater: nil, // No updater configured at all.
	}
	status, _ := postApplyUpdate(t, s, map[string]string{"channel": "latest"})
	assert.Equal(t, 400, status)
}

// Ensure the fake satisfies the Updater interface.
var _ updater.Updater = (*fakeUpdater)(nil)
