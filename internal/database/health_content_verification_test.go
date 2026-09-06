package database

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A healthy result normally clears error_details. The content-verification
// outcome is the exception: without it a verified-clean file is
// indistinguishable from one that was never probed.
func TestUpdateHealthStatusBulk_HealthyPersistsContentVerification(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, last_error, error_details)
		VALUES ('verified.mkv', 'pending', 'stale error', '{"error_type":"missing_segments"}')
	`)
	require.NoError(t, err)

	details := HealthErrorDetails{ContentVerification: ContentVerificationPassed}
	err = repo.UpdateHealthStatusBulk(ctx, []HealthStatusUpdate{{
		Type:             UpdateTypeHealthy,
		FilePath:         "verified.mkv",
		ScheduledCheckAt: time.Now().UTC(),
		ErrorDetails:     details.Marshal(),
	}})
	require.NoError(t, err)

	fh, err := repo.GetFileHealth(ctx, "verified.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, fh.Status)
	assert.Nil(t, fh.LastError, "a healthy record must not keep a stale error message")
	require.NotNil(t, fh.ErrorDetails)
	assert.JSONEq(t, `{"content_verification":"passed"}`, *fh.ErrorDetails)
}

// Without a verification outcome the healthy write must clear the column
// exactly as it always did.
func TestUpdateHealthStatusBulk_HealthyClearsDetailsWhenUnverified(t *testing.T) {
	repo := setupTestDB(t)
	ctx := context.Background()

	_, err := repo.db.ExecContext(ctx, `
		INSERT INTO file_health (file_path, status, last_error, error_details)
		VALUES ('plain.mkv', 'pending', 'stale error', '{"error_type":"missing_segments"}')
	`)
	require.NoError(t, err)

	err = repo.UpdateHealthStatusBulk(ctx, []HealthStatusUpdate{{
		Type:             UpdateTypeHealthy,
		FilePath:         "plain.mkv",
		ScheduledCheckAt: time.Now().UTC(),
	}})
	require.NoError(t, err)

	fh, err := repo.GetFileHealth(ctx, "plain.mkv")
	require.NoError(t, err)
	assert.Equal(t, HealthStatusHealthy, fh.Status)
	assert.Nil(t, fh.ErrorDetails, "a healthy result with nothing to report must clear error_details")
}
