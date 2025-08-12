package database

import (
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConcurrentQueueOperations(t *testing.T) {
	// Create temporary database
	tempDB := "/tmp/test_concurrent_queue.sqlite"
	defer os.Remove(tempDB)

	// Create database
	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository

	// Test concurrent queue insertions
	const numGoroutines = 10
	const itemsPerGoroutine = 20
	
	var wg sync.WaitGroup
	start := time.Now()

	// Simulate concurrent scanning operations adding items to queue
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()

			for j := 0; j < itemsPerGoroutine; j++ {
				item := &ImportQueueItem{
					NzbPath:    fmt.Sprintf("/path/to/file_%d_%d.nzb", goroutineID, j),
					Priority:   QueuePriorityNormal,
					Status:     QueueStatusPending,
					RetryCount: 0,
					MaxRetries: 3,
					CreatedAt:  time.Now(),
					UpdatedAt:  time.Now(),
				}

				if err := repo.AddToQueue(item); err != nil {
					t.Errorf("Goroutine %d failed to add item %d: %v", goroutineID, j, err)
					return
				}
			}
		}(i)
	}

	wg.Wait()
	duration := time.Since(start)

	t.Logf("✅ Concurrent insertions completed in %v", duration)
	t.Logf("   - %d goroutines, %d items each = %d total items", numGoroutines, itemsPerGoroutine, numGoroutines*itemsPerGoroutine)
	t.Logf("   - Average: %.2f items/second", float64(numGoroutines*itemsPerGoroutine)/duration.Seconds())

	// Verify all items were inserted
	stats, err := repo.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get queue stats: %v", err)
	}

	expectedTotal := numGoroutines * itemsPerGoroutine
	if stats.TotalQueued != expectedTotal {
		t.Errorf("Expected %d queued items, got %d", expectedTotal, stats.TotalQueued)
	}

	// Test concurrent processing simulation
	const numWorkers = 5
	start = time.Now()

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				// Get next items (simulating worker behavior)
				items, err := repo.GetNextQueueItems(1)
				if err != nil {
					t.Errorf("Worker %d failed to get queue items: %v", workerID, err)
					return
				}

				if len(items) == 0 {
					return // No more work
				}

				item := items[0]

				// Use immediate transaction for status updates (like real processing)
				if err := repo.WithImmediateTransaction(func(txRepo *Repository) error {
					return txRepo.UpdateQueueItemStatus(item.ID, QueueStatusCompleted, nil)
				}); err != nil {
					t.Errorf("Worker %d failed to update status: %v", workerID, err)
					return
				}
			}
		}(w)
	}

	wg.Wait()
	processingDuration := time.Since(start)

	t.Logf("✅ Concurrent processing completed in %v", processingDuration)
	t.Logf("   - %d workers processed %d items", numWorkers, expectedTotal)
	t.Logf("   - Average: %.2f items/second", float64(expectedTotal)/processingDuration.Seconds())

	// Verify all items were processed
	finalStats, err := repo.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get final queue stats: %v", err)
	}

	if finalStats.TotalCompleted != expectedTotal {
		t.Errorf("Expected %d completed items, got %d", expectedTotal, finalStats.TotalCompleted)
	}

	if finalStats.TotalQueued != 0 {
		t.Errorf("Expected 0 queued items after processing, got %d", finalStats.TotalQueued)
	}
}

func TestBatchInsertPerformance(t *testing.T) {
	// Create temporary database
	tempDB := "/tmp/test_batch_insert.sqlite"
	defer os.Remove(tempDB)

	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		t.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository
	const numItems = 1000

	// Test individual inserts
	items1 := make([]*ImportQueueItem, numItems)
	for i := 0; i < numItems; i++ {
		items1[i] = &ImportQueueItem{
			NzbPath:    fmt.Sprintf("/path/to/individual_%d.nzb", i),
			Priority:   QueuePriorityNormal,
			Status:     QueueStatusPending,
			RetryCount: 0,
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}
	}

	start := time.Now()
	for _, item := range items1 {
		if err := repo.AddToQueue(item); err != nil {
			t.Fatalf("Failed individual insert: %v", err)
		}
	}
	individualDuration := time.Since(start)

	// Clean up
	db.Connection().Exec("DELETE FROM import_queue")

	// Test batch insert
	items2 := make([]*ImportQueueItem, numItems)
	for i := 0; i < numItems; i++ {
		items2[i] = &ImportQueueItem{
			NzbPath:    fmt.Sprintf("/path/to/batch_%d.nzb", i),
			Priority:   QueuePriorityNormal,
			Status:     QueueStatusPending,
			RetryCount: 0,
			MaxRetries: 3,
		}
	}

	start = time.Now()
	if err := repo.AddBatchToQueue(items2); err != nil {
		t.Fatalf("Failed batch insert: %v", err)
	}
	batchDuration := time.Since(start)

	speedup := float64(individualDuration) / float64(batchDuration)

	t.Logf("✅ Batch insert performance comparison:")
	t.Logf("   - Individual inserts: %v (%.2f items/second)", individualDuration, float64(numItems)/individualDuration.Seconds())
	t.Logf("   - Batch insert: %v (%.2f items/second)", batchDuration, float64(numItems)/batchDuration.Seconds())
	t.Logf("   - Speedup: %.2fx faster", speedup)

	if speedup < 2.0 {
		t.Logf("⚠️  Expected at least 2x speedup, got %.2fx", speedup)
	}
}

func BenchmarkConcurrentQueueOperations(b *testing.B) {
	tempDB := "/tmp/benchmark_concurrent_queue.sqlite"
	defer os.Remove(tempDB)

	config := Config{DatabasePath: tempDB}
	db, err := New(config)
	if err != nil {
		b.Fatalf("Failed to create database: %v", err)
	}
	defer db.Close()

	repo := db.Repository

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		workerID := 0
		itemCount := 0
		for pb.Next() {
			item := &ImportQueueItem{
				NzbPath:    fmt.Sprintf("/bench/worker_%d_item_%d.nzb", workerID, itemCount),
				Priority:   QueuePriorityNormal,
				Status:     QueueStatusPending,
				RetryCount: 0,
				MaxRetries: 3,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}

			if err := repo.AddToQueue(item); err != nil {
				b.Errorf("Failed to add queue item: %v", err)
			}
			itemCount++
		}
	})
}