package database

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestAtomicQueueClaiming verifies that ClaimNextQueueItem prevents duplicate processing
func TestAtomicQueueClaiming(t *testing.T) {
	// Create temporary database
	tempDB := "/tmp/test_atomic_claiming.sqlite"
	defer os.Remove(tempDB)

	// Create database
	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository

	// Add test items to queue
	const numItems = 10
	for i := 0; i < numItems; i++ {
		item := &ImportQueueItem{
			NzbPath:    fmt.Sprintf("/test/file_%d.nzb", i),
			Priority:   QueuePriorityNormal,
			Status:     QueueStatusPending,
			RetryCount: 0,
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := repo.AddToQueue(item); err != nil {
			t.Fatalf("Failed to add item %d: %v", i, err)
		}
	}

	// Test concurrent claiming - should prevent duplicates
	const numWorkers = 5
	var wg sync.WaitGroup
	claimedItems := make(chan *ImportQueueItem, numItems)
	errorsChan := make(chan error, numWorkers*numItems)

	start := time.Now()

	// Start workers that try to claim items concurrently
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Each worker tries to claim items until none are available
			for {
				item, err := repo.ClaimNextQueueItem()
				if err != nil {
					// Database locked errors are expected in high concurrency scenarios
					if strings.Contains(err.Error(), "database is locked") {
						time.Sleep(1 * time.Millisecond) // Brief backoff and retry
						continue
					}
					errorsChan <- fmt.Errorf("Worker %d claim error: %v", workerID, err)
					return
				}

				if item == nil {
					// No more items to process
					return
				}

				// Simulate some processing time
				time.Sleep(1 * time.Millisecond)

				// Send claimed item to channel for verification
				claimedItems <- item

				// Mark as completed using immediate transaction
				if err := repo.WithImmediateTransaction(func(txRepo *Repository) error {
					return txRepo.UpdateQueueItemStatus(item.ID, QueueStatusCompleted, nil)
				}); err != nil {
					errorsChan <- fmt.Errorf("Worker %d failed to complete item %d: %v", workerID, item.ID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	close(claimedItems)
	close(errorsChan)

	duration := time.Since(start)

	// Check for any errors
	for err := range errorsChan {
		t.Errorf("Worker error: %v", err)
	}

	// Collect all claimed items and verify no duplicates
	claimedMap := make(map[int64]bool)
	claimedCount := 0

	for item := range claimedItems {
		if claimedMap[item.ID] {
			t.Errorf("Duplicate processing detected! Item ID %d was processed multiple times", item.ID)
		}
		claimedMap[item.ID] = true
		claimedCount++
	}

	t.Logf("✅ Atomic claiming test completed in %v", duration)
	t.Logf("   - %d workers, %d items", numWorkers, numItems)
	t.Logf("   - %d items claimed successfully", claimedCount)
	t.Logf("   - No duplicate processing detected")

	// Verify all items were processed exactly once
	if claimedCount != numItems {
		t.Errorf("Expected %d items to be processed, got %d", numItems, claimedCount)
	}

	// Verify final queue state
	stats, err := repo.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get final queue stats: %v", err)
	}

	if stats.TotalCompleted != numItems {
		t.Errorf("Expected %d completed items, got %d", numItems, stats.TotalCompleted)
	}

	if stats.TotalQueued != 0 {
		t.Errorf("Expected 0 queued items after processing, got %d", stats.TotalQueued)
	}
}

// TestRaceConditionPrevention specifically tests the race condition that was causing duplicates
func TestRaceConditionPrevention(t *testing.T) {
	tempDB := "/tmp/test_race_condition.sqlite"
	defer os.Remove(tempDB)

	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository

	// Add a single item to test race condition
	item := &ImportQueueItem{
		NzbPath:    "/test/race_test.nzb",
		Priority:   QueuePriorityNormal,
		Status:     QueueStatusPending,
		RetryCount: 0,
		MaxRetries: 3,
		CreatedAt:  time.Now(),
		UpdatedAt:  time.Now(),
	}

	if err := repo.AddToQueue(item); err != nil {
		t.Fatalf("Failed to add test item: %v", err)
	}

	// Start multiple workers simultaneously trying to claim the same item
	const numWorkers = 10
	var wg sync.WaitGroup
	claimedItems := make(chan *ImportQueueItem, numWorkers)

	start := time.Now()

	// All workers start at exactly the same time to maximize race condition potential
	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			// Try to claim the item
			claimedItem, err := repo.ClaimNextQueueItem()
			if err != nil {
				t.Errorf("Worker %d claim error: %v", workerID, err)
				return
			}

			if claimedItem != nil {
				claimedItems <- claimedItem
				t.Logf("Worker %d successfully claimed item %d", workerID, claimedItem.ID)
			}
		}(w)
	}

	wg.Wait()
	close(claimedItems)

	duration := time.Since(start)

	// Count how many workers successfully claimed the item
	claimCount := 0
	var claimedItem *ImportQueueItem
	for item := range claimedItems {
		claimCount++
		claimedItem = item
	}

	t.Logf("✅ Race condition test completed in %v", duration)
	t.Logf("   - %d workers attempted to claim 1 item simultaneously", numWorkers)
	t.Logf("   - %d workers successfully claimed the item", claimCount)

	// The critical test: exactly one worker should have successfully claimed the item
	if claimCount != 1 {
		t.Errorf("Race condition detected! Expected exactly 1 worker to claim the item, got %d", claimCount)
	}

	// Verify the item status was updated correctly
	if claimedItem != nil {
		if claimedItem.Status != QueueStatusProcessing {
			t.Errorf("Claimed item should have status 'processing', got %s", claimedItem.Status)
		}
	}
}

// BenchmarkAtomicClaiming measures the performance of atomic claiming
func BenchmarkAtomicClaiming(b *testing.B) {
	tempDB := "/tmp/benchmark_atomic_claiming.sqlite"
	defer os.Remove(tempDB)

	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository

	// Pre-populate with test items
	for i := 0; i < b.N; i++ {
		item := &ImportQueueItem{
			NzbPath:    fmt.Sprintf("/bench/file_%d.nzb", i),
			Priority:   QueuePriorityNormal,
			Status:     QueueStatusPending,
			RetryCount: 0,
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := repo.AddToQueue(item); err != nil {
			b.Fatalf("Failed to add benchmark item: %v", err)
		}
	}

	b.ResetTimer()

	// Benchmark the atomic claiming operation
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			item, err := repo.ClaimNextQueueItem()
			if err != nil {
				b.Errorf("Claim error: %v", err)
				continue
			}
			if item == nil {
				// No more items to process
				continue
			}
		}
	})
}