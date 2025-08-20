package health

import (
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHealthChecker(t *testing.T) {
	// Create in-memory database for testing
	db, err := database.NewDB(database.Config{DatabasePath: ":memory:"})
	require.NoError(t, err)
	defer db.Close()

	repo := database.NewHealthRepository(db.Connection())
	
	// Create a temporary metadata service for testing
	metadataService := metadata.NewMetadataService("./test-metadata")
	
	checker := NewHealthChecker(repo, metadataService, nil, HealthCheckerConfig{
		MaxRetries: 2,
	})

	// Ensure checker was created successfully
	assert.NotNil(t, checker)

	t.Run("HealthRepository operations", func(t *testing.T) {
		// Test direct repository operations
		err := repo.UpdateFileHealth("test-file-1", database.HealthStatusHealthy, nil, nil, nil)
		assert.NoError(t, err)

		// Verify the status was recorded
		status, err := repo.GetFileHealth("test-file-1")
		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, database.HealthStatusHealthy, status.Status)
	})

	t.Run("Repository retry operations", func(t *testing.T) {
		// Add a corrupted file
		errorMsg := "test error"
		err := repo.UpdateFileHealth("corrupted-file", database.HealthStatusCorrupted, &errorMsg, nil, nil)
		require.NoError(t, err)
		
		// Simulate incrementing retry count
		err = repo.IncrementRetryCount("corrupted-file", &errorMsg)
		assert.NoError(t, err)

		// Verify retry count was incremented
		status, err := repo.GetFileHealth("corrupted-file")
		assert.NoError(t, err)
		assert.NotNil(t, status)
		assert.Equal(t, 1, status.RetryCount)
	})

	t.Run("Health stats", func(t *testing.T) {
		// Get health statistics
		stats, err := repo.GetHealthStats()
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		
		// Should have at least the files we created
		total := 0
		for _, count := range stats {
			total += count
		}
		assert.GreaterOrEqual(t, total, 2) // At least test-file-1 and corrupted-file
	})
}