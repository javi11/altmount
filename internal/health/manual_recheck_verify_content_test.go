package health

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/stretchr/testify/require"
)

// TestPerformBackgroundCheck_ThreadsVerifyContentOverride confirms a manual
// recheck's verify_content override reaches the checker even for a file
// whose status is not Pending — where the automatic Pending-only gate alone
// would have skipped content verification.
func TestPerformBackgroundCheck_ThreadsVerifyContentOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	client := fakepool.New()
	env := newBatchTestEnv(t, t.TempDir(), client)

	enabled := true
	cfg := env.hw.configGetter()
	cfg.Health.VerifyContent = &enabled

	env.healthChecker.contentVerifyFS = &fakeContentOpener{data: make([]byte, 512)} // no recognized signature

	path := "complete/movie.mkv"
	writeHealthyFile(t, env, path)

	_, err := env.db.Exec(`
		INSERT INTO file_health (file_path, status, retry_count, max_retries, repair_retry_count, max_repair_retries, scheduled_check_at)
		VALUES (?, 'degraded', 0, 3, 0, 3, datetime('now', '-1 second'))
	`, path)
	require.NoError(t, err)

	env.hw.mu.Lock()
	env.hw.running = true
	env.hw.mu.Unlock()

	force := true
	require.NoError(t, env.hw.PerformBackgroundCheck(context.Background(), path, database.HealthStatusDegraded, &force))

	// A definitive content failure routes through the same retry-before-repair
	// path as any other corrupted verdict (see prepareUpdateForResult), so the
	// first failure lands back on Pending with retry_count incremented rather
	// than immediately Corrupted. What proves the override reached the checker
	// is the recorded error_details carrying the content_invalid error type.
	require.Eventually(t, func() bool {
		fh, err := env.healthRepo.GetFileHealth(context.Background(), path)
		return err == nil && fh != nil && fh.ErrorDetails != nil && strings.Contains(*fh.ErrorDetails, "content_invalid")
	}, 2*time.Second, 10*time.Millisecond, "override must force content verification despite the Degraded status")
}

// TestPerformBackgroundCheck_AutoVerifiesContentOnPendingWithoutOverride is the
// regression test for the manual-recheck path being unreachable by the automatic
// Pending-only content-verification gate: without any verify_content override, a
// manual recheck of a file whose status was Pending BEFORE the caller transitioned
// it to Checking must still run content verification. Passing the stale
// post-transition status ("checking") into CheckOptions.CurrentStatus would make
// shouldVerifyContent's Pending-only gate permanently unreachable on this path.
func TestPerformBackgroundCheck_AutoVerifiesContentOnPendingWithoutOverride(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks not supported on Windows")
	}

	client := fakepool.New()
	env := newBatchTestEnv(t, t.TempDir(), client)

	enabled := true
	cfg := env.hw.configGetter()
	cfg.Health.VerifyContent = &enabled

	env.healthChecker.contentVerifyFS = &fakeContentOpener{data: make([]byte, 512)} // no recognized signature

	path := "complete/movie.mkv"
	writeHealthyFile(t, env, path)

	_, err := env.db.Exec(`
		INSERT INTO file_health (file_path, status, retry_count, max_retries, repair_retry_count, max_repair_retries, scheduled_check_at)
		VALUES (?, 'pending', 0, 3, 0, 3, datetime('now', '-1 second'))
	`, path)
	require.NoError(t, err)

	env.hw.mu.Lock()
	env.hw.running = true
	env.hw.mu.Unlock()

	// Mirrors handleDirectHealthCheck: the pre-transition status (Pending) is
	// captured, the row is then transitioned to Checking, and no verify_content
	// override is supplied.
	require.NoError(t, env.hw.healthRepo.SetFileChecking(context.Background(), path))
	require.NoError(t, env.hw.PerformBackgroundCheck(context.Background(), path, database.HealthStatusPending, nil))

	// See TestPerformBackgroundCheck_ThreadsVerifyContentOverride: a definitive
	// content failure lands back on Pending with error_details carrying
	// content_invalid, rather than immediately Corrupted.
	require.Eventually(t, func() bool {
		fh, err := env.healthRepo.GetFileHealth(context.Background(), path)
		return err == nil && fh != nil && fh.ErrorDetails != nil && strings.Contains(*fh.ErrorDetails, "content_invalid")
	}, 2*time.Second, 10*time.Millisecond, "a Pending file's manual recheck must auto-verify content without an explicit override")
}
