package rclonecli

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// newMountTestManager builds a minimal Manager wired with the fields the mount
// retry and unmount paths touch, plus injectable restart/mountFn/forceUnmount
// seams so the retry and VFS-reclamation decisions can be asserted without a
// live rcd subprocess.
func newMountTestManager(t *testing.T) (*Manager, *int32) {
	t.Helper()

	ready := make(chan struct{})
	close(ready) // IsReady() == true

	var restartCalls int32
	m := &Manager{
		logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:           context.Background(),
		rcPort:        "5572",
		mounts:        make(map[string]*MountInfo),
		serverReady:   ready,
		serverStarted: true,
		httpClient:    &http.Client{Timeout: 500 * time.Millisecond},
		forceUnmount: func(string) error {
			return nil
		},
		retryDelay: func(int) time.Duration {
			return time.Millisecond
		},
	}
	m.restart = func(context.Context) error {
		atomic.AddInt32(&restartCalls, 1)
		return nil
	}
	return m, &restartCalls
}

func TestUnmount_RestartFailureIsReturned(t *testing.T) {
	m, _ := newMountTestManager(t)
	m.mounts["altmount"] = &MountInfo{Provider: "altmount", LocalPath: "/mnt/test", Mounted: true}
	m.restart = func(context.Context) error {
		return errors.New("restart failed")
	}

	err := m.unmount(context.Background(), "altmount", true)
	if err == nil || !strings.Contains(err.Error(), "restart failed") {
		t.Fatalf("expected restart failure, got: %v", err)
	}
}

func TestWaitForReadyAcceptsReadyServerWhileRestarting(t *testing.T) {
	m, _ := newMountTestManager(t)
	m.mu.Lock()
	m.restarting = true
	m.mu.Unlock()

	if err := m.WaitForReady(time.Second); err != nil {
		t.Fatalf("ready rcd must not be rejected while restart bookkeeping is finishing: %v", err)
	}
}

func TestMountWithRetry_SuccessOnFirstAttempt(t *testing.T) {
	m, restarts := newMountTestManager(t)
	m.mountFn = func(context.Context, string, string, string) error {
		return nil
	}

	if err := m.mountWithRetry(context.Background(), "altmount", "/mnt/test", "http://localhost:2020/webdav", 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("no restart expected on first-attempt success, got %d", got)
	}
}

func TestMountWithRetry_FirstAttemptFailsThenSucceeds(t *testing.T) {
	m, restarts := newMountTestManager(t)
	var attempts int32
	m.mountFn = func(context.Context, string, string, string) error {
		if atomic.AddInt32(&attempts, 1) == 1 {
			return errors.New("fusermount: exit status 1")
		}
		return nil
	}

	if err := m.mountWithRetry(context.Background(), "altmount", "/mnt/test", "http://localhost:2020/webdav", 3); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 2 {
		t.Fatalf("expected 2 mount attempts, got %d", got)
	}
	// The rcd must be restarted between attempts so the leaked VFS from the
	// failed attempt is cleared before the retry.
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Fatalf("expected 1 restart between attempts, got %d", got)
	}
}

func TestMountWithRetry_AllAttemptsFailRestartsEachTimeAndFinalCleanup(t *testing.T) {
	m, restarts := newMountTestManager(t)
	wantErr := errors.New("failed to mount FUSE fs: fusermount: exit status 1")
	m.mountFn = func(context.Context, string, string, string) error {
		return wantErr
	}

	err := m.mountWithRetry(context.Background(), "altmount", "/mnt/test", "http://localhost:2020/webdav", 3)
	if err == nil {
		t.Fatal("expected error from mount retry")
	}
	if !strings.Contains(err.Error(), wantErr.Error()) {
		t.Fatalf("expected wrapped mount error, got: %v", err)
	}
	// One restart before each of the 3 retries plus one after the terminal
	// failure to leave the rcd free of leaked VFS state.
	if got := atomic.LoadInt32(restarts); got != 4 {
		t.Fatalf("expected 4 restarts (3 between attempts + 1 final cleanup), got %d", got)
	}
}

func TestMountWithRetry_CleanupRestartFailureAbortsRetry(t *testing.T) {
	m, restarts := newMountTestManager(t)
	// Override restart to fail. Retrying against the same dirty rcd would leak
	// another VFS, so mountWithRetry must stop immediately.
	m.restart = func(context.Context) error {
		atomic.AddInt32(restarts, 1)
		return errors.New("failed to restart rcd")
	}
	var attempts int32
	m.mountFn = func(context.Context, string, string, string) error {
		if atomic.AddInt32(&attempts, 1) < 3 {
			return errors.New("transient failure")
		}
		return nil
	}

	err := m.mountWithRetry(context.Background(), "altmount", "/mnt/test", "http://localhost:2020/webdav", 3)
	if err == nil || !strings.Contains(err.Error(), "cleanup failed") {
		t.Fatalf("expected cleanup failure, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected retry to stop after the first dirty attempt, got %d attempts", got)
	}
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Fatalf("expected 1 restart attempt, got %d", got)
	}
}

func TestMountWithRetry_ContextCancelledDuringRetry(t *testing.T) {
	m, _ := newMountTestManager(t)
	var attempts int32
	m.mountFn = func(context.Context, string, string, string) error {
		atomic.AddInt32(&attempts, 1)
		return errors.New("boom")
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := m.mountWithRetry(ctx, "altmount", "/mnt/test", "http://localhost:2020/webdav", 3)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got != 1 {
		t.Fatalf("expected a single mount attempt before cancellation, got %d", got)
	}
}

func TestUnmount_RestartsRCDToReclaimVFS(t *testing.T) {
	m, restarts := newMountTestManager(t)
	m.mounts["altmount"] = &MountInfo{Provider: "altmount", LocalPath: "/mnt/test", Mounted: true}

	if err := m.unmount(context.Background(), "altmount", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info, ok := m.GetMountInfo("altmount")
	if !ok || info.Mounted {
		t.Fatal("mount must be marked unmounted after Unmount")
	}
	// rclone retains the VFS in-process after mount/unmount; a restart is
	// required so a later mount does not create a second VFS instance.
	if got := atomic.LoadInt32(restarts); got != 1 {
		t.Fatalf("expected rcd restart after unmount, got %d", got)
	}
}

func TestUnmount_NoRestartWhenRCDDisabled(t *testing.T) {
	m, restarts := newMountTestManager(t)
	m.mounts["altmount"] = &MountInfo{Provider: "altmount", LocalPath: "/mnt/test", Mounted: true}

	if err := m.unmount(context.Background(), "altmount", false); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("no restart expected during shutdown unmount, got %d", got)
	}
}

func TestUnmount_AlreadyUnmountedNoRestart(t *testing.T) {
	m, restarts := newMountTestManager(t)
	m.mounts["altmount"] = &MountInfo{Provider: "altmount", LocalPath: "/mnt/test", Mounted: false}

	if err := m.unmount(context.Background(), "altmount", true); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := atomic.LoadInt32(restarts); got != 0 {
		t.Fatalf("no restart expected for already-unmounted provider, got %d", got)
	}
}
