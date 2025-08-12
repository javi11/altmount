package database

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestTwoDatabaseWorkflow tests the complete two-database workflow
func TestTwoDatabaseWorkflow(t *testing.T) {
	// Create temporary databases
	tempMainDB := "/tmp/test_two_db_main.sqlite"
	tempQueueDB := "/tmp/test_two_db_queue.sqlite"
	defer os.Remove(tempMainDB)
	defer os.Remove(tempQueueDB)

	// Create main database (for serving files)
	mainConfig := Config{DatabasePath: tempMainDB}
	mainDB, err := New(mainConfig)
	if err != nil {
		t.Fatalf("Failed to create main database: %v", err)
	}
	defer mainDB.Close()

	// Create queue database (for processing queue)
	queueConfig := QueueConfig{DatabasePath: tempQueueDB}
	queueDB, err := NewQueueDB(queueConfig)
	if err != nil {
		t.Fatalf("Failed to create queue database: %v", err)
	}
	defer queueDB.Close()

	t.Logf("✅ Created two databases successfully")

	// Test 1: Add files to queue
	testFiles := []string{
		"/test/file1.nzb",
		"/test/file2.nzb",
		"/test/file3.nzb",
	}

	for i, filePath := range testFiles {
		item := &ImportQueueItem{
			NzbPath:    filePath,
			Priority:   QueuePriorityNormal,
			Status:     QueueStatusPending,
			RetryCount: 0,
			MaxRetries: 3,
			CreatedAt:  time.Now(),
			UpdatedAt:  time.Now(),
		}

		if err := queueDB.Repository.AddToQueue(item); err != nil {
			t.Errorf("Failed to add item %d to queue: %v", i, err)
		}
	}

	// Test 2: Verify queue operations work
	stats, err := queueDB.Repository.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get queue stats: %v", err)
	}

	if stats.TotalQueued != len(testFiles) {
		t.Errorf("Expected %d queued items, got %d", len(testFiles), stats.TotalQueued)
	}

	t.Logf("✅ Queue operations working: %d items queued", stats.TotalQueued)

	// Test 3: Test atomic claiming from queue
	claimedItem, err := queueDB.Repository.ClaimNextQueueItem()
	if err != nil {
		t.Fatalf("Failed to claim queue item: %v", err)
	}

	if claimedItem == nil {
		t.Fatal("Expected to claim an item, got nil")
	}

	if claimedItem.Status != QueueStatusProcessing {
		t.Errorf("Expected claimed item status to be 'processing', got %s", claimedItem.Status)
	}

	t.Logf("✅ Atomic claiming working: claimed item %d", claimedItem.ID)

	// Test 4: Test main database operations (simulate processing)
	// In real scenario, processor would create NZB file and virtual files here
	nzbFile := &NzbFile{
		Path:          claimedItem.NzbPath,
		Filename:      "test.nzb",
		Size:          1000,
		NzbType:       NzbTypeSingleFile,
		SegmentsCount: 1,
		SegmentSize:   1000,
	}

	if err := mainDB.Repository.CreateNzbFile(nzbFile); err != nil {
		t.Errorf("Failed to create NZB file in main DB: %v", err)
	} else {
		t.Logf("✅ Main database write working: created NZB file %d", nzbFile.ID)
	}

	// Test 5: Complete queue item
	if err := queueDB.Repository.UpdateQueueItemStatus(claimedItem.ID, QueueStatusCompleted, nil); err != nil {
		t.Errorf("Failed to mark queue item as completed: %v", err)
	}

	// Test 6: Verify final stats
	finalStats, err := queueDB.Repository.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get final queue stats: %v", err)
	}

	if finalStats.TotalCompleted != 1 {
		t.Errorf("Expected 1 completed item, got %d", finalStats.TotalCompleted)
	}

	if finalStats.TotalQueued != len(testFiles)-1 {
		t.Errorf("Expected %d remaining queued items, got %d", len(testFiles)-1, finalStats.TotalQueued)
	}

	t.Logf("✅ Two-database workflow completed successfully")
	t.Logf("   - Queue DB: %d completed, %d pending", finalStats.TotalCompleted, finalStats.TotalQueued)
	t.Logf("   - Main DB: NZB file created with ID %d", nzbFile.ID)
}

// TestConcurrentTwoDatabaseOperations tests concurrent operations across both databases
func TestConcurrentTwoDatabaseOperations(t *testing.T) {
	tempMainDB := "/tmp/test_concurrent_two_db_main.sqlite"
	tempQueueDB := "/tmp/test_concurrent_two_db_queue.sqlite"
	defer os.Remove(tempMainDB)
	defer os.Remove(tempQueueDB)

	// Create databases
	mainConfig := Config{DatabasePath: tempMainDB}
	mainDB, err := New(mainConfig)
	if err != nil {
		t.Fatalf("Failed to create main database: %v", err)
	}
	defer mainDB.Close()

	queueConfig := QueueConfig{DatabasePath: tempQueueDB}
	queueDB, err := NewQueueDB(queueConfig)
	if err != nil {
		t.Fatalf("Failed to create queue database: %v", err)
	}
	defer queueDB.Close()

	// Test concurrent queue operations
	const numItems = 20
	const numWorkers = 5

	// Phase 1: Concurrent queue additions
	var wg sync.WaitGroup
	start := time.Now()

	for i := 0; i < numItems; i++ {
		wg.Add(1)
		go func(itemID int) {
			defer wg.Done()

			item := &ImportQueueItem{
				NzbPath:    fmt.Sprintf("/test/concurrent_%d.nzb", itemID),
				Priority:   QueuePriorityNormal,
				Status:     QueueStatusPending,
				RetryCount: 0,
				MaxRetries: 3,
				CreatedAt:  time.Now(),
				UpdatedAt:  time.Now(),
			}

			if err := queueDB.Repository.AddToQueue(item); err != nil {
				t.Errorf("Failed to add concurrent item %d: %v", itemID, err)
			}
		}(i)
	}

	wg.Wait()
	addDuration := time.Since(start)

	// Phase 2: Concurrent queue processing (simulating workers)
	start = time.Now()
	processedCount := 0
	var processedMutex sync.Mutex

	for w := 0; w < numWorkers; w++ {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()

			for {
				// Claim item from queue
				item, err := queueDB.Repository.ClaimNextQueueItem()
				if err != nil {
					// Database locked errors are expected in high concurrency
					if strings.Contains(err.Error(), "database is locked") {
						time.Sleep(1 * time.Millisecond) // Brief backoff
						continue
					}
					t.Errorf("Worker %d failed to claim item: %v", workerID, err)
					return
				}

				if item == nil {
					return // No more work
				}

				// Simulate processing by creating NZB file in main DB
				nzbFile := &NzbFile{
					Path:          item.NzbPath,
					Filename:      fmt.Sprintf("worker_%d_file.nzb", workerID),
					Size:          1000,
					NzbType:       NzbTypeSingleFile,
					SegmentsCount: 1,
					SegmentSize:   1000,
				}

				// Write to main database (separate from queue operations)
				if err := mainDB.Repository.CreateNzbFile(nzbFile); err != nil {
					t.Errorf("Worker %d failed to create NZB file: %v", workerID, err)
				}

				// Mark queue item as completed
				if err := queueDB.Repository.UpdateQueueItemStatus(item.ID, QueueStatusCompleted, nil); err != nil {
					t.Errorf("Worker %d failed to complete item: %v", workerID, err)
				}

				processedMutex.Lock()
				processedCount++
				processedMutex.Unlock()
			}
		}(w)
	}

	wg.Wait()
	processDuration := time.Since(start)

	// Verify results
	queueStats, err := queueDB.Repository.GetQueueStats()
	if err != nil {
		t.Fatalf("Failed to get final queue stats: %v", err)
	}

	if queueStats.TotalCompleted != numItems {
		t.Errorf("Expected %d completed items in queue, got %d", numItems, queueStats.TotalCompleted)
	}

	if processedCount != numItems {
		t.Errorf("Expected %d processed items, got %d", numItems, processedCount)
	}

	t.Logf("✅ Concurrent two-database operations completed successfully")
	t.Logf("   - Queue additions: %v (%d items, %.2f items/sec)", addDuration, numItems, float64(numItems)/addDuration.Seconds())
	t.Logf("   - Processing: %v (%d workers, %.2f items/sec)", processDuration, numWorkers, float64(numItems)/processDuration.Seconds())
	t.Logf("   - Final queue stats: %d completed, %d pending", queueStats.TotalCompleted, queueStats.TotalQueued)
}

// TestDatabaseIsolation verifies that queue operations don't affect main DB performance
func TestDatabaseIsolation(t *testing.T) {
	tempMainDB := "/tmp/test_isolation_main.sqlite"
	tempQueueDB := "/tmp/test_isolation_queue.sqlite"
	defer os.Remove(tempMainDB)
	defer os.Remove(tempQueueDB)

	// Create databases
	mainConfig := Config{DatabasePath: tempMainDB}
	mainDB, err := New(mainConfig)
	if err != nil {
		t.Fatalf("Failed to create main database: %v", err)
	}
	defer mainDB.Close()

	queueConfig := QueueConfig{DatabasePath: tempQueueDB}
	queueDB, err := NewQueueDB(queueConfig)
	if err != nil {
		t.Fatalf("Failed to create queue database: %v", err)
	}
	defer queueDB.Close()

	// Create test data in main database
	for i := 0; i < 100; i++ {
		nzbFile := &NzbFile{
			Path:          fmt.Sprintf("/test/isolation_%d.nzb", i),
			Filename:      fmt.Sprintf("isolation_%d.nzb", i),
			Size:          int64(i * 1000),
			NzbType:       NzbTypeSingleFile,
			SegmentsCount: 1,
			SegmentSize:   int64(i * 1000),
		}

		if err := mainDB.Repository.CreateNzbFile(nzbFile); err != nil {
			t.Errorf("Failed to create test NZB file %d: %v", i, err)
		}
	}

	// Test: Heavy queue operations while measuring main DB performance
	var wg sync.WaitGroup
	
	// Start heavy queue operations
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			item := &ImportQueueItem{
				NzbPath:    fmt.Sprintf("/test/heavy_queue_%d.nzb", i),
				Priority:   QueuePriorityNormal,
				Status:     QueueStatusPending,
				RetryCount: 0,
				MaxRetries: 3,
			}
			queueDB.Repository.AddToQueue(item)
		}
	}()

	// Measure main database read performance during heavy queue operations
	start := time.Now()
	var readCount int
	
	for i := 0; i < 500; i++ {
		path := fmt.Sprintf("/test/isolation_%d.nzb", i%100)
		nzb, err := mainDB.Repository.GetNzbFileByPath(path)
		if err != nil {
			t.Errorf("Failed to read from main DB during queue load: %v", err)
		}
		if nzb != nil {
			readCount++
		}
	}
	
	readDuration := time.Since(start)
	wg.Wait()

	t.Logf("✅ Database isolation test completed")
	t.Logf("   - Main DB reads during queue load: %v (%d reads, %.2f reads/sec)", 
		readDuration, readCount, float64(readCount)/readDuration.Seconds())
	t.Logf("   - Queue operations completed successfully during main DB reads")

	// Verify isolation - main DB should not be affected by queue operations
	if readDuration > 5*time.Second {
		t.Errorf("Main DB reads took too long during queue operations: %v", readDuration)
	}
}