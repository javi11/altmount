package queue

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
)

// ItemProcessor defines the interface for processing queue items
type ItemProcessor interface {
	// ProcessItem processes a single queue item and returns the resulting path or an error
	ProcessItem(ctx context.Context, item *database.ImportQueueItem) (string, error)
	// HandleSuccess handles successful processing
	HandleSuccess(ctx context.Context, item *database.ImportQueueItem, resultingPath string) error
	// HandleFailure handles failed processing
	HandleFailure(ctx context.Context, item *database.ImportQueueItem, err error)
}

// ManagerConfig holds configuration for the queue manager
type ManagerConfig struct {
	Workers      int
	ConfigGetter config.ConfigGetter
}

// Manager manages queue workers and processing
type Manager struct {
	config       ManagerConfig
	repository   *database.QueueRepository
	claimer      *Claimer
	processor    ItemProcessor
	configGetter config.ConfigGetter
	log          *slog.Logger

	// Runtime state
	mu      sync.RWMutex
	running bool
	paused  bool
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	// Cancellation tracking for processing items
	cancelFuncs map[int64]context.CancelFunc
	cancelMu    sync.RWMutex
}

// NewManager creates a new queue manager
func NewManager(cfg ManagerConfig, repository *database.QueueRepository, processor ItemProcessor) *Manager {
	if cfg.Workers == 0 {
		cfg.Workers = 2
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &Manager{
		config:       cfg,
		repository:   repository,
		claimer:      NewClaimer(repository),
		processor:    processor,
		configGetter: cfg.ConfigGetter,
		log:          slog.Default().With("component", "queue-manager"),
		ctx:          ctx,
		cancel:       cancel,
		cancelFuncs:  make(map[int64]context.CancelFunc),
	}
}

// Start starts the queue workers
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return nil
	}

	// Start worker pool
	for i := 0; i < m.config.Workers; i++ {
		m.wg.Add(1)
		go m.workerLoop(i)
	}

	m.running = true
	m.log.InfoContext(ctx, "Queue manager started", "workers", m.config.Workers)

	return nil
}

// Stop stops the queue workers
func (m *Manager) Stop(ctx context.Context) error {
	m.mu.Lock()

	if !m.running {
		m.mu.Unlock()
		return nil
	}

	m.log.InfoContext(ctx, "Stopping queue manager")

	// Cancel all goroutines
	m.cancel()
	m.running = false
	m.mu.Unlock()

	// Wait for all goroutines to finish with timeout
	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-time.After(30 * time.Second):
		m.log.WarnContext(ctx, "Timeout waiting for workers to stop")
	case <-ctx.Done():
		m.log.WarnContext(ctx, "Context cancelled while waiting for workers")
		return ctx.Err()
	}

	// Re-acquire lock to recreate context for potential restart
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx, m.cancel = context.WithCancel(context.Background())

	m.log.InfoContext(ctx, "Queue manager stopped")
	return nil
}

// Pause pauses queue processing
func (m *Manager) Pause() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = true
	m.log.InfoContext(m.ctx, "Queue manager paused")
}

// Resume resumes queue processing
func (m *Manager) Resume() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.paused = false
	m.log.InfoContext(m.ctx, "Queue manager resumed")
}

// IsPaused returns whether the manager is paused
func (m *Manager) IsPaused() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.paused
}

// IsRunning returns whether the manager is running
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

// CancelProcessing cancels processing for a specific item
func (m *Manager) CancelProcessing(itemID int64) error {
	m.cancelMu.RLock()
	cancel, exists := m.cancelFuncs[itemID]
	m.cancelMu.RUnlock()

	if !exists {
		return nil // Not currently processing
	}

	m.log.InfoContext(m.ctx, "Cancelling processing for queue item", "item_id", itemID)
	cancel()
	return nil
}

// workerLoop is the main worker loop
func (m *Manager) workerLoop(workerID int) {
	defer m.wg.Done()

	log := m.log.With("worker_id", workerID)

	// Get processing interval from configuration
	processingIntervalSeconds := m.configGetter().Import.QueueProcessingIntervalSeconds
	processingInterval := time.Duration(processingIntervalSeconds) * time.Second

	ticker := time.NewTicker(processingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Check if manager is paused
			if m.IsPaused() {
				continue
			}
			m.processNextItem(m.ctx, workerID)
		case <-m.ctx.Done():
			log.Info("Queue worker stopped")
			return
		}
	}
}

// processNextItem claims and processes the next queue item
func (m *Manager) processNextItem(ctx context.Context, workerID int) {
	// Claim next available item
	item, err := m.claimer.ClaimWithRetry(ctx, workerID)
	if err != nil {
		// Only log non-contention errors
		if !IsDatabaseContentionError(err) {
			m.log.ErrorContext(ctx, "Failed to claim next queue item", "worker_id", workerID, "error", err)
		}
		return
	}

	if item == nil {
		return // No work to do
	}

	m.log.DebugContext(ctx, "Processing claimed queue item", "worker_id", workerID, "queue_id", item.ID, "file", item.NzbPath)

	// Create cancellable context for this item
	itemCtx, cancel := context.WithCancel(ctx)

	// Register cancel function
	m.cancelMu.Lock()
	m.cancelFuncs[item.ID] = cancel
	m.cancelMu.Unlock()

	// Clean up after processing
	defer func() {
		m.cancelMu.Lock()
		delete(m.cancelFuncs, item.ID)
		m.cancelMu.Unlock()
	}()

	// Process the item
	resultingPath, processingErr := m.processor.ProcessItem(itemCtx, item)

	// Handle results
	if processingErr != nil {
		m.processor.HandleFailure(ctx, item, processingErr)
	} else {
		m.processor.HandleSuccess(ctx, item, resultingPath)
	}
}
