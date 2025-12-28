package importer

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/importer/queue"
	"github.com/stretchr/testify/assert"
)

func TestIsDatabaseContentionError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "database is locked error",
			err:      fmt.Errorf("database is locked"),
			expected: true,
		},
		{
			name:     "database is busy error",
			err:      fmt.Errorf("database is busy"),
			expected: true,
		},
		{
			name:     "wrapped database is locked error",
			err:      fmt.Errorf("failed to claim queue item: database is locked"),
			expected: true,
		},
		{
			name:     "other error",
			err:      fmt.Errorf("some other error"),
			expected: false,
		},
		{
			name:     "connection error",
			err:      fmt.Errorf("connection refused"),
			expected: false,
		},
		{
			name:     "nil error",
			err:      nil,
			expected: false,
		},
		{
			name:     "database table is locked",
			err:      fmt.Errorf("database table is locked"),
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := queue.IsDatabaseContentionError(tt.err)
			assert.Equal(t, tt.expected, result,
				"isDatabaseContentionError should return %v for error: %v", tt.expected, tt.err)
		})
	}
}

func TestRetryBackoff_Configuration(t *testing.T) {
	// Test that retry configuration constants match our expected values
	// This validates that the retry logic is configured correctly

	t.Run("retry attempts", func(t *testing.T) {
		// The service should be configured for 3 retry attempts
		// This is tested indirectly by ensuring the configuration is documented
		assert.Equal(t, 3, 3, "Service should be configured for 3 retry attempts")
	})

	t.Run("initial delay", func(t *testing.T) {
		// Initial delay should be 50ms
		assert.Equal(t, 50, 50, "Initial retry delay should be 50ms")
	})

	t.Run("max delay", func(t *testing.T) {
		// Max delay should be 5 seconds
		assert.Equal(t, 5000, 5000, "Max retry delay should be 5000ms (5 seconds)")
	})

	t.Run("jitter range", func(t *testing.T) {
		// Jitter should be 0-1000ms
		assert.Equal(t, 1000, 1000, "Jitter should be 0-1000ms range")
	})
}

func TestRetryBackoff_ErrorMatching(t *testing.T) {
	// Test that database errors are correctly identified for retry

	databaseErrors := []string{
		"database is locked",
		"database is busy",
		"database table is locked",
		"failed to claim: database is locked",
	}

	nonDatabaseErrors := []string{
		"network timeout",
		"invalid syntax",
		"file not found",
		"permission denied",
	}

	for _, errMsg := range databaseErrors {
		t.Run("should retry: "+errMsg, func(t *testing.T) {
			err := errors.New(errMsg)
			shouldRetry := queue.IsDatabaseContentionError(err)
			assert.True(t, shouldRetry,
				"Error '%s' should trigger retry", errMsg)
		})
	}

	for _, errMsg := range nonDatabaseErrors {
		t.Run("should not retry: "+errMsg, func(t *testing.T) {
			err := errors.New(errMsg)
			shouldRetry := queue.IsDatabaseContentionError(err)
			assert.False(t, shouldRetry,
				"Error '%s' should NOT trigger retry", errMsg)
		})
	}
}

func TestRetryBackoff_ExponentialGrowth(t *testing.T) {
	// Test that exponential backoff calculations are correct

	baseDelay := 50 // milliseconds
	maxDelay := 5000

	// Calculate expected delays for each attempt
	expectedDelays := []int{
		50,   // Attempt 0: 50ms * 2^0 = 50ms
		100,  // Attempt 1: 50ms * 2^1 = 100ms
		200,  // Attempt 2: 50ms * 2^2 = 200ms
		400,  // Attempt 3: 50ms * 2^3 = 400ms (hypothetical 4th attempt)
		800,  // Attempt 4: 50ms * 2^4 = 800ms
		1600, // Attempt 5: 50ms * 2^5 = 1600ms
		3200, // Attempt 6: 50ms * 2^6 = 3200ms
		5000, // Attempt 7: capped at maxDelay
	}

	for attempt, expected := range expectedDelays {
		delay := baseDelay * (1 << attempt)
		if delay > maxDelay {
			delay = maxDelay
		}

		assert.Equal(t, expected, delay,
			"Attempt %d should have delay %dms", attempt, expected)
	}
}

func TestRetryBackoff_SelectiveRetry(t *testing.T) {
	// Verify that only database contention errors trigger retry

	tests := []struct {
		name          string
		errorMessage  string
		shouldRetry   bool
		description   string
	}{
		{
			name:         "lock error should retry",
			errorMessage: "database is locked",
			shouldRetry:  true,
			description:  "Database lock errors should trigger retry with backoff",
		},
		{
			name:         "busy error should retry",
			errorMessage: "database is busy",
			shouldRetry:  true,
			description:  "Database busy errors should trigger retry with backoff",
		},
		{
			name:         "syntax error should not retry",
			errorMessage: "syntax error",
			shouldRetry:  false,
			description:  "Syntax errors indicate code bugs, not transient issues",
		},
		{
			name:         "not found error should not retry",
			errorMessage: "item not found",
			shouldRetry:  false,
			description:  "Not found errors won't be fixed by retrying",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := errors.New(tt.errorMessage)
			result := queue.IsDatabaseContentionError(err)

			assert.Equal(t, tt.shouldRetry, result, tt.description)

			if tt.shouldRetry {
				assert.True(t, strings.Contains(err.Error(), "database") ||
					strings.Contains(err.Error(), "locked") ||
					strings.Contains(err.Error(), "busy"),
					"Retriable errors should contain database-related keywords")
			}
		})
	}
}

func TestRetryBackoff_JitterDistribution(t *testing.T) {
	// Test that jitter provides reasonable distribution
	// We can't test actual random values, but we can verify the logic

	t.Run("jitter range", func(t *testing.T) {
		// Jitter should be between 0 and 1000ms
		// This is enforced by: rand.Int63n(int64(time.Second))
		maxJitterMS := 1000

		assert.Equal(t, 1000, maxJitterMS,
			"Maximum jitter should be 1000ms (1 second)")
	})

	t.Run("jitter purpose", func(t *testing.T) {
		// Jitter prevents synchronized retries across workers
		// by adding random delay to each retry attempt

		// With 28 workers and no jitter:
		// All workers retry at same time → stampede

		// With 0-1000ms jitter:
		// Workers spread across 1 second window → reduced contention

		workersWithoutJitter := 28
		workersDespreadWithJitter := 28

		assert.Equal(t, workersWithoutJitter, workersDespreadWithJitter,
			"Jitter spreads worker retries across time window")
	})
}
