package rclonecli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/httpclient"
)

// Manager handles the rclone RC server and provides mount operations
type Manager struct {
	cmd           *exec.Cmd
	rcPort        string
	rcloneDir     string
	mounts        map[string]*MountInfo
	mountsMutex   sync.RWMutex
	logger        *slog.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	httpClient     *http.Client
	serverReady    chan struct{}
	processExited  chan struct{}
	serverStarted  bool
	mu             sync.RWMutex
	cfg            *config.Manager
}

type MountInfo struct {
	Provider   string `json:"provider"`
	LocalPath  string `json:"local_path"`
	WebDAVURL  string `json:"webdav_url"`
	Mounted    bool   `json:"mounted"`
	MountedAt  string `json:"mounted_at,omitempty"`
	ConfigName string `json:"config_name"`
	Error      string `json:"error,omitempty"`
}

type RCRequest struct {
	Command string         `json:"command"`
	Args    map[string]any `json:"args,omitempty"`
}

type RCResponse struct {
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// NewManager creates a new rclone RC manager
func NewManager(cfm *config.Manager) *Manager {
	cfg := cfm.GetConfig()
	logger := slog.Default().With("component", "rclone")

	rcPort := fmt.Sprintf("%d", cfg.RClone.RCPort)
	rcloneDir := filepath.Join(cfg.RClone.Path, "rclone")

	// Ensure config directory exists
	if err := os.MkdirAll(rcloneDir, 0755); err != nil {
		logger.ErrorContext(context.Background(), "Failed to create rclone config directory", "err", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		cfg:         cfm,
		rcPort:      rcPort,
		rcloneDir:   rcloneDir,
		mounts:      make(map[string]*MountInfo),
		logger:      logger,
		ctx:         ctx,
		cancel:      cancel,
		httpClient:    httpclient.NewDefault(),
		serverReady:   make(chan struct{}),
		processExited: make(chan struct{}),
	}
}

// Start starts the rclone RC server
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.serverStarted {
		return nil
	}

	cfg := m.cfg.GetConfig()
	if !*cfg.RClone.RCEnabled {
		m.logger.InfoContext(ctx, "Rclone is disabled, skipping RC server startup")
		return nil
	}

	// Get log directory from config, fallback to current directory
	logDir := filepath.Dir(cfg.Log.File)
	if logDir == "." || logDir == "" {
		logDir = "."
	}
	logFile := filepath.Join(logDir, "rclone.log")

	// Delete old log file if it exists
	if _, err := os.Stat(logFile); err == nil {
		if err := os.Remove(logFile); err != nil {
			return fmt.Errorf("failed to remove old rclone log file: %w", err)
		}
	}

	args := []string{
		"rcd",
		"--rc-addr", ":" + m.rcPort,
		"--rc-no-auth", // We'll handle auth at the application level
		"--config", filepath.Join(m.rcloneDir, "rclone.conf"),
		"--log-file", logFile,
		"--use-cookies",
	}

	logLevel := cfg.RClone.LogLevel
	if logLevel != "" {
		if !slices.Contains([]string{"DEBUG", "INFO", "NOTICE", "ERROR"}, logLevel) {
			logLevel = "INFO"
		}
		args = append(args, "--log-level", logLevel)
	}

	if cfg.RClone.CacheDir != "" {
		if err := os.MkdirAll(cfg.RClone.CacheDir, 0755); err == nil {
			args = append(args, "--cache-dir", cfg.RClone.CacheDir)
		}
	}

	m.logger.InfoContext(ctx, "Starting rclone RC server", "args", args)

	m.cmd = exec.CommandContext(ctx, "rclone", args...)

	// Capture output for debugging
	var stdout, stderr bytes.Buffer
	m.cmd.Stdout = &stdout
	m.cmd.Stderr = &stderr

	if err := m.cmd.Start(); err != nil {
		m.logger.ErrorContext(ctx, "Failed to start rclone RC server", "stderr", stderr.String(), "stdout", stdout.String(), "err", err)
		return fmt.Errorf("failed to start rclone RC server: %w", err)
	}
	m.serverStarted = true

	// Capture references owned by this lifecycle so a future restart that
	// swaps these channels under m.mu cannot race with the goroutine below.
	cmd := m.cmd
	serverReady := m.serverReady
	processExited := m.processExited

	// Wait for server to be ready in a goroutine
	go func() {
		defer func() {
			if r := recover(); r != nil {
				m.logger.ErrorContext(m.ctx, "Panic in rclone RC server monitor", "panic", r)
			}
		}()

		m.waitForServer()
		close(serverReady)

		// Start mount monitoring once server is ready
		go func() {
			defer func() {
				if r := recover(); r != nil {
					m.logger.ErrorContext(m.ctx, "Panic in mount monitor", "panic", r)
				}
			}()
			m.MonitorMounts(ctx)
		}()

		// Wait for command to finish and log output
		err := cmd.Wait()
		// Signal that the subprocess has exited so restartServer can proceed.
		select {
		case <-processExited:
			// already closed (defensive)
		default:
			close(processExited)
		}
		switch {
		case err == nil:
			m.logger.InfoContext(m.ctx, "Rclone RC server exited normally")

		case errors.Is(err, context.Canceled):
			m.logger.InfoContext(m.ctx, "Rclone RC server terminated: context canceled")

		case WasHardTerminated(err): // SIGKILL on *nix; non-zero exit on Windows
			m.logger.InfoContext(m.ctx, "Rclone RC server hard-terminated")

		default:
			if code, ok := ExitCode(err); ok {
				m.logger.DebugContext(m.ctx, "Rclone RC server error", "exit_code", code, "err", err, "stderr", stderr.String(), "stdout", stdout.String())
			} else {
				m.logger.DebugContext(m.ctx, "Rclone RC server error (no exit code)", "err", err, "stderr", stderr.String(), "stdout", stdout.String())
			}
		}
	}()
	return nil
}

// Stop stops the rclone RC server and unmounts all mounts
func (m *Manager) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.serverStarted {
		return nil
	}

	m.logger.InfoContext(m.ctx, "Stopping rclone RC server")

	// Unmount all mounts first
	m.mountsMutex.RLock()
	mountList := make([]*MountInfo, 0, len(m.mounts))
	for _, mount := range m.mounts {
		if mount.Mounted {
			mountList = append(mountList, mount)
		}
	}
	m.mountsMutex.RUnlock()

	// Unmount in parallel
	var wg sync.WaitGroup
	for _, mount := range mountList {
		wg.Add(1)
		go func(mount *MountInfo) {
			defer wg.Done()
			if err := m.unmount(m.ctx, mount.Provider); err != nil {
				m.logger.ErrorContext(m.ctx, "Failed to unmount during shutdown", "err", err, "provider", mount.Provider)
			}
		}(mount)
	}

	// Wait for unmounts with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		m.logger.InfoContext(m.ctx, "All mounts unmounted successfully")
	case <-time.After(30 * time.Second):
		m.logger.WarnContext(m.ctx, "Timeout waiting for mounts to unmount, proceeding with shutdown")
	}

	// Cancel context and stop process
	m.cancel()

	if m.cmd != nil && m.cmd.Process != nil {
		// Try graceful shutdown first
		if err := m.cmd.Process.Signal(os.Interrupt); err != nil {
			m.logger.WarnContext(m.ctx, "Failed to send interrupt signal, using kill", "err", err)
			if killErr := m.cmd.Process.Kill(); killErr != nil {
				m.logger.ErrorContext(m.ctx, "Failed to kill rclone process", "err", killErr)
				return killErr
			}
		}

		// Wait for process to exit with timeout
		done := make(chan error, 1)
		go func() {
			done <- m.cmd.Wait()
		}()

		select {
		case err := <-done:
			if err != nil && !errors.Is(err, context.Canceled) && !WasHardTerminated(err) {
				m.logger.WarnContext(m.ctx, "Rclone process exited with error", "err", err)
			}
		case <-time.After(10 * time.Second):
			m.logger.WarnContext(m.ctx, "Timeout waiting for rclone to exit, force killing")
			if err := m.cmd.Process.Kill(); err != nil {
				m.logger.ErrorContext(m.ctx, "Failed to force kill rclone process", "err", err)
				return err
			}
			// Wait a bit more for the kill to take effect
			select {
			case <-done:
				m.logger.InfoContext(m.ctx, "Rclone process killed successfully")
			case <-time.After(5 * time.Second):
				m.logger.ErrorContext(m.ctx, "Process may still be running after kill")
			}
		}
	}

	// Clean up any remaining mount directories
	cfg := m.cfg.GetConfig()
	if cfg.MountPath != "" {
		m.cleanupMountDirectories(cfg.MountPath)
	}

	m.serverStarted = false
	m.logger.InfoContext(m.ctx, "Rclone RC server stopped")
	return nil
}

// cleanupMountDirectories removes empty mount directories
func (m *Manager) cleanupMountDirectories(_ string) {
	m.mountsMutex.RLock()
	defer m.mountsMutex.RUnlock()

	for _, mount := range m.mounts {
		if mount.LocalPath != "" {
			// Try to remove the directory if it's empty
			if err := os.Remove(mount.LocalPath); err == nil {
				m.logger.DebugContext(m.ctx, "Removed empty mount directory", "path", mount.LocalPath)
			}
			// Don't log errors here as the directory might not be empty, which is fine
		}
	}
}

// waitForServer waits for the RC server to become available
func (m *Manager) waitForServer() {
	maxAttempts := 30
	for range maxAttempts {
		if err := m.ctx.Err(); err != nil {
			return
		}

		if m.pingServer() {
			m.logger.InfoContext(m.ctx, "Rclone RC server is ready")
			return
		}

		time.Sleep(time.Second)
	}

	m.logger.ErrorContext(m.ctx, "Rclone RC server not responding - mount operations will be disabled")
}

// pingServer checks if the RC server is responding
func (m *Manager) pingServer() bool {
	req := RCRequest{
		Command: "core/version",
		Args:    map[string]any{},
	}
	_, err := m.makeRequest(req, true)
	return err == nil
}

func (m *Manager) makeRequest(req RCRequest, close bool) (*http.Response, error) {
	reqBody, err := json.Marshal(req.Args)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := fmt.Sprintf("http://localhost:%s/%s", m.rcPort, req.Command)
	httpReq, err := http.NewRequestWithContext(m.ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to make request: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// Read the response body to get more details
		defer resp.Body.Close()
		var errorResp RCResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			return nil, fmt.Errorf("request failed with status %s, but could not decode error response: %w", resp.Status, err)
		}
		if errorResp.Error != "" {
			return nil, fmt.Errorf("%s", errorResp.Error)
		} else {
			return nil, fmt.Errorf("request failed with status %s and no error message", resp.Status)
		}
	}

	if close {
		defer func() {
			if err := resp.Body.Close(); err != nil {
				m.logger.DebugContext(m.ctx, "Failed to close response body", "err", err)
			}
		}()
	}

	return resp, nil
}

// IsReady returns true if the RC server is ready
func (m *Manager) IsReady() bool {
	select {
	case <-m.serverReady:
		return true
	default:
		return false
	}
}

// WaitForReady waits for the RC server to be ready
func (m *Manager) WaitForReady(timeout time.Duration) error {
	select {
	case <-m.serverReady:
		return nil
	case <-time.After(timeout):
		return fmt.Errorf("timeout waiting for rclone RC server to be ready")
	case <-m.ctx.Done():
		return m.ctx.Err()
	}
}

func (m *Manager) GetLogger() *slog.Logger {
	return m.logger
}

// pingServerWithTimeout probes the rcd server with a bounded timeout so a
// wedged subprocess fails fast instead of hanging the recovery path.
// Returns true if rcd answers core/version within the timeout.
func (m *Manager) pingServerWithTimeout(ctx context.Context, timeout time.Duration) bool {
	probeCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, err := json.Marshal(map[string]any{})
	if err != nil {
		return false
	}

	url := fmt.Sprintf("http://localhost:%s/core/version", m.rcPort)
	req, err := http.NewRequestWithContext(probeCtx, http.MethodPost, url, bytes.NewBuffer(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return false
	}
	defer func() {
		if cerr := resp.Body.Close(); cerr != nil {
			m.logger.DebugContext(probeCtx, "Failed to close ping response body", "err", cerr)
		}
	}()
	return resp.StatusCode == http.StatusOK
}

// restartServer force-kills the rcd subprocess and starts a fresh one.
// Used by recovery paths when the rcd has wedged and is not responding to RPCs.
// The mount map is preserved; callers are expected to re-establish mounts
// against the fresh rcd via Mount(...).
func (m *Manager) restartServer(ctx context.Context) error {
	m.mu.Lock()
	cmd := m.cmd
	exited := m.processExited
	wasStarted := m.serverStarted
	m.mu.Unlock()

	if !wasStarted || cmd == nil || cmd.Process == nil {
		return fmt.Errorf("rcd subprocess not running, cannot restart")
	}

	m.logger.WarnContext(ctx, "Killing wedged rcd subprocess", "pid", cmd.Process.Pid)
	if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		m.logger.ErrorContext(ctx, "Failed to kill rcd process", "err", err)
		// Continue anyway — the Wait goroutine may still close processExited
		// if the process eventually dies on its own.
	}

	// Wait for the existing Start goroutine to observe the exit, with a hard
	// cap so a stuck Wait doesn't block recovery indefinitely.
	select {
	case <-exited:
	case <-time.After(10 * time.Second):
		m.logger.WarnContext(ctx, "Timed out waiting for rcd subprocess to exit; proceeding with restart")
	case <-ctx.Done():
		return ctx.Err()
	}

	// Reset lifecycle state so Start() will spawn a new subprocess.
	m.mu.Lock()
	m.serverStarted = false
	m.cmd = nil
	m.serverReady = make(chan struct{})
	m.processExited = make(chan struct{})
	m.mu.Unlock()

	if err := m.Start(ctx); err != nil {
		return fmt.Errorf("failed to restart rcd: %w", err)
	}

	if err := m.WaitForReady(30 * time.Second); err != nil {
		return fmt.Errorf("rcd did not become ready after restart: %w", err)
	}

	// Mark all known mounts as unmounted so the next health-check tick
	// re-establishes them against the fresh rcd.
	m.mountsMutex.Lock()
	for _, mount := range m.mounts {
		mount.Mounted = false
		mount.Error = "rcd subprocess restarted; awaiting remount"
	}
	m.mountsMutex.Unlock()

	m.logger.InfoContext(ctx, "rcd subprocess restarted successfully")
	return nil
}
