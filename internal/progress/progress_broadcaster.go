package progress

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// ProgressUpdate represents a progress update event
type ProgressUpdate struct {
	QueueID    int       `json:"queue_id"`
	Percentage int       `json:"percentage"`
	Stage      string    `json:"stage,omitempty"`  // e.g. "Parsing NZB", "Validating segments"
	Status     string    `json:"status,omitempty"` // "completed" or "failed" on terminal events
	Timestamp  time.Time `json:"timestamp"`
}

// ProgressEntry holds the current progress state for a single queue item.
// It is returned by GetAllProgress for the SSE initial payload.
type ProgressEntry struct {
	Percentage int    `json:"percentage"`
	Stage      string `json:"stage,omitempty"`
}

// progressState is the internal progress record (unexported).
type progressState struct {
	percentage int
	stage      string
}

// ProgressBroadcaster manages progress tracking for queue items
type ProgressBroadcaster struct {
	progress    map[int]progressState
	mu          sync.RWMutex
	log         *slog.Logger
	subscribers map[string]chan ProgressUpdate
	subMu       sync.RWMutex
}

// NewProgressBroadcaster creates a new progress broadcaster
func NewProgressBroadcaster() *ProgressBroadcaster {
	pb := &ProgressBroadcaster{
		progress:    make(map[int]progressState),
		subscribers: make(map[string]chan ProgressUpdate),
		log:         slog.Default().With("component", "progress-broadcaster"),
	}

	return pb
}

func (pb *ProgressBroadcaster) Close() error {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()
	for _, ch := range pb.subscribers {
		close(ch)
	}
	pb.subscribers = make(map[string]chan ProgressUpdate)

	return nil
}

// UpdateProgress updates the progress for a queue item (no stage label).
func (pb *ProgressBroadcaster) UpdateProgress(queueID int, percentage int) {
	pb.UpdateProgressWithStage(queueID, percentage, "")
}

// UpdateProgressWithStage updates the progress for a queue item with an optional stage label.
func (pb *ProgressBroadcaster) UpdateProgressWithStage(queueID int, percentage int, stage string) {
	// Clamp percentage to 0-100 range
	if percentage < 0 {
		percentage = 0
	}
	if percentage > 100 {
		percentage = 100
	}

	pb.mu.Lock()
	if percentage >= 100 {
		// Remove progress when complete (100%)
		delete(pb.progress, queueID)
	} else {
		pb.progress[queueID] = progressState{percentage: percentage, stage: stage}
	}
	pb.mu.Unlock()

	// Broadcast update to all SSE subscribers
	update := ProgressUpdate{
		QueueID:    queueID,
		Percentage: percentage,
		Stage:      stage,
		Timestamp:  time.Now(),
	}

	pb.subMu.RLock()
	for subID, ch := range pb.subscribers {
		select {
		case ch <- update:
		default:
			pb.log.WarnContext(context.Background(), "subscriber channel full, skipping update", "subscriber_id", subID, "queue_id", queueID)
		}
	}
	pb.subMu.RUnlock()
}

// NotifyComplete broadcasts a terminal completion or failure event for a queue item
// and removes it from progress tracking. status should be "completed" or "failed".
func (pb *ProgressBroadcaster) NotifyComplete(queueID int, status string) {
	pb.mu.Lock()
	delete(pb.progress, queueID)
	pb.mu.Unlock()

	update := ProgressUpdate{
		QueueID:   queueID,
		Status:    status,
		Timestamp: time.Now(),
	}

	pb.subMu.RLock()
	for subID, ch := range pb.subscribers {
		select {
		case ch <- update:
		default:
			pb.log.WarnContext(context.Background(), "subscriber channel full, skipping completion event",
				"subscriber_id", subID, "queue_id", queueID)
		}
	}
	pb.subMu.RUnlock()
}

// ClearProgress removes progress tracking for a completed or failed queue item
func (pb *ProgressBroadcaster) ClearProgress(queueID int) {
	pb.mu.Lock()
	delete(pb.progress, queueID)
	pb.mu.Unlock()
}

// GetProgress returns the current progress percentage for a queue item.
func (pb *ProgressBroadcaster) GetProgress(queueID int) (int, bool) {
	pb.mu.RLock()
	defer pb.mu.RUnlock()
	state, exists := pb.progress[queueID]
	return state.percentage, exists
}

// GetAllProgress returns a copy of all current progress states including stage labels.
// The returned map is used to build the initial SSE payload sent to new subscribers.
func (pb *ProgressBroadcaster) GetAllProgress() map[int]ProgressEntry {
	pb.mu.RLock()
	defer pb.mu.RUnlock()

	result := make(map[int]ProgressEntry, len(pb.progress))
	for id, state := range pb.progress {
		result[id] = ProgressEntry{Percentage: state.percentage, Stage: state.stage}
	}
	return result
}

// CreateTracker creates a progress tracker for a specific queue item with a percentage range
func (pb *ProgressBroadcaster) CreateTracker(queueID, minPercent, maxPercent int) *Tracker {
	return NewTracker(pb, queueID, minPercent, maxPercent)
}

// Subscribe creates a new SSE subscriber and returns a subscription ID and update channel
func (pb *ProgressBroadcaster) Subscribe() (string, <-chan ProgressUpdate) {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()

	subID := fmt.Sprintf("sub-%d", time.Now().UnixNano())
	ch := make(chan ProgressUpdate, 10)
	pb.subscribers[subID] = ch

	return subID, ch
}

// Unsubscribe removes an SSE subscriber and closes its channel
func (pb *ProgressBroadcaster) Unsubscribe(subID string) {
	pb.subMu.Lock()
	defer pb.subMu.Unlock()

	if ch, exists := pb.subscribers[subID]; exists {
		close(ch)
		delete(pb.subscribers, subID)
	}
}

// HasSubscribers reports whether at least one SSE client is connected.
func (pb *ProgressBroadcaster) HasSubscribers() bool {
	pb.subMu.RLock()
	defer pb.subMu.RUnlock()
	return len(pb.subscribers) > 0
}

// BroadcastHealthChanged sends a health-change notification to all SSE subscribers.
// Uses QueueID=0 and Status="health_changed" as a sentinel for health state changes.
func (pb *ProgressBroadcaster) BroadcastHealthChanged() {
	update := ProgressUpdate{
		QueueID:   0,
		Status:    "health_changed",
		Timestamp: time.Now(),
	}
	pb.subMu.RLock()
	defer pb.subMu.RUnlock()
	for subID, ch := range pb.subscribers {
		select {
		case ch <- update:
		default:
			pb.log.WarnContext(context.Background(),
				"subscriber channel full, skipping health_changed", "subscriber_id", subID)
		}
	}
}

// BroadcastQueueChanged sends a queue-change notification to all SSE subscribers.
// Uses QueueID=0 and Status="queue_changed" as a sentinel for non-progress queue events.
func (pb *ProgressBroadcaster) BroadcastQueueChanged() {
	update := ProgressUpdate{
		QueueID:   0,
		Status:    "queue_changed",
		Timestamp: time.Now(),
	}
	pb.subMu.RLock()
	defer pb.subMu.RUnlock()
	for subID, ch := range pb.subscribers {
		select {
		case ch <- update:
		default:
			pb.log.WarnContext(context.Background(),
				"subscriber channel full, skipping queue_changed", "subscriber_id", subID)
		}
	}
}
