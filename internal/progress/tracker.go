package progress

// Broadcaster interface for updating progress
type Broadcaster interface {
	UpdateProgress(queueID int, percentage int)
}

// ProgressTracker interface for types that can report progress
type ProgressTracker interface {
	Update(current, total int)
	UpdateAbsolute(percentage int)
}

// Tracker encapsulates progress updates for a specific queue item
type Tracker struct {
	queueID     int
	broadcaster Broadcaster
	minPercent  int
	maxPercent  int
}

// NewTracker creates a progress tracker for a specific queue item with a percentage range
func NewTracker(broadcaster Broadcaster, queueID, minPercent, maxPercent int) *Tracker {
	return &Tracker{
		queueID:     queueID,
		broadcaster: broadcaster,
		minPercent:  minPercent,
		maxPercent:  maxPercent,
	}
}

// Update reports progress within the configured percentage range.
// Safe to call on a nil receiver (no-op).
func (pt *Tracker) Update(current, total int) {
	if pt == nil || pt.broadcaster == nil {
		return
	}
	if total > 0 {
		rangeSize := pt.maxPercent - pt.minPercent
		percentage := pt.minPercent + (current * rangeSize / total)
		pt.broadcaster.UpdateProgress(pt.queueID, percentage)
	}
}

// UpdateAbsolute reports an absolute percentage value, bypassing the tracker's range
// This is useful for final progress updates (e.g., 100%) when the tracker's range
// doesn't cover the full 0-100% spectrum
func (pt *Tracker) UpdateAbsolute(percentage int) {
	if pt != nil && pt.broadcaster != nil {
		pt.broadcaster.UpdateProgress(pt.queueID, percentage)
	}
}

// OffsetTracker wraps a base tracker and adds an offset to progress updates.
// This is useful for cumulative progress tracking across multiple sequential operations
// where each operation reports progress from 0→N, but we want overall progress.
//
// Example: Processing 3 files with 100, 50, 50 segments (200 total):
//
//	File 1: OffsetTracker{offset: 0, total: 200} → updates 0/200, 1/200, ..., 100/200
//	File 2: OffsetTracker{offset: 100, total: 200} → updates 100/200, 101/200, ..., 150/200
//	File 3: OffsetTracker{offset: 150, total: 200} → updates 150/200, 151/200, ..., 200/200
type OffsetTracker struct {
	baseTracker *Tracker
	offset      int
	total       int
}

// NewOffsetTracker creates a progress tracker that adds an offset to all updates.
// The offset represents work completed before this tracker's scope, and total represents
// the overall work across all operations.
func NewOffsetTracker(baseTracker *Tracker, offset, total int) *OffsetTracker {
	return &OffsetTracker{
		baseTracker: baseTracker,
		offset:      offset,
		total:       total,
	}
}

// Update reports progress by adding the offset to current before delegating to base tracker.
// This maintains cumulative progress across multiple sequential operations.
func (ot *OffsetTracker) Update(current, total int) {
	if ot != nil && ot.baseTracker != nil {
		// Add offset to current for cumulative progress
		cumulativeCurrent := ot.offset + current
		ot.baseTracker.Update(cumulativeCurrent, ot.total)
	}
}

// UpdateAbsolute delegates absolute percentage updates to the base tracker.
func (ot *OffsetTracker) UpdateAbsolute(percentage int) {
	if ot != nil && ot.baseTracker != nil {
		ot.baseTracker.UpdateAbsolute(percentage)
	}
}
