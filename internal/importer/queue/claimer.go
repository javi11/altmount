// Package queue provides queue management for the NZB import service.
package queue

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"
	"time"

	"github.com/avast/retry-go/v4"
	"github.com/javi11/altmount/internal/database"
)

// QueueRepository defines the interface for queue database operations
type QueueRepository interface {
	ClaimNextQueueItem(ctx context.Context) (*database.ImportQueueItem, error)
}

// Claimer handles claiming queue items with retry logic
type Claimer struct {
	repo QueueRepository
	log  *slog.Logger
}

// NewClaimer creates a new Claimer
func NewClaimer(repo QueueRepository) *Claimer {
	return &Claimer{
		repo: repo,
		log:  slog.Default().With("component", "queue-claimer"),
	}
}

// ClaimWithRetry attempts to claim a queue item with exponential backoff retry logic
func (c *Claimer) ClaimWithRetry(ctx context.Context, workerID int) (*database.ImportQueueItem, error) {
	var item *database.ImportQueueItem

	err := retry.Do(
		func() error {
			claimedItem, err := c.repo.ClaimNextQueueItem(ctx)
			if err != nil {
				return err
			}

			item = claimedItem
			return nil
		},
		retry.Attempts(3),                // Reduced from 5 - immediate transactions should succeed quickly
		retry.Delay(50*time.Millisecond), // Increased from 10ms
		retry.MaxDelay(5*time.Second),    // Increased from 500ms to allow better spreading
		retry.DelayType(retry.BackOffDelay),
		retry.RetryIf(IsDatabaseContentionError),
		retry.OnRetry(func(n uint, err error) {
			// Add jitter to prevent synchronized retries across workers
			// Jitter range: 0-1000ms to desynchronize worker retries
			jitter := time.Duration(rand.Int63n(int64(time.Second)))
			time.Sleep(jitter)

			// Calculate exponential backoff for logging
			baseDelay := 50 * time.Millisecond
			backoffDelay := baseDelay * (1 << n) // Exponential: 50ms, 100ms, 200ms...
			if backoffDelay > 5*time.Second {
				backoffDelay = 5 * time.Second
			}

			// Only log warnings after first retry to reduce noise
			if n >= 1 {
				c.log.WarnContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"backoff_ms", backoffDelay.Milliseconds(),
					"jitter_ms", jitter.Milliseconds(),
					"error", err)
			} else {
				c.log.DebugContext(ctx, "Database contention, retrying claim",
					"attempt", n+1,
					"worker_id", workerID,
					"backoff_ms", backoffDelay.Milliseconds(),
					"jitter_ms", jitter.Milliseconds(),
					"error", err)
			}
		}),
	)

	if err != nil {
		return nil, fmt.Errorf("failed to claim queue item: %w", err)
	}

	if item == nil {
		return nil, nil
	}

	c.log.DebugContext(ctx, "Next item in processing queue", "queue_id", item.ID, "file", item.NzbPath)
	return item, nil
}

// IsDatabaseContentionError checks if an error is a retryable database contention error
func IsDatabaseContentionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "database is locked") ||
		strings.Contains(errStr, "database is busy") ||
		strings.Contains(errStr, "database table is locked")
}
