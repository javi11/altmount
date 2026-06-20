package progress

// Broadcaster interface for updating progress
type Broadcaster interface {
	UpdateProgress(queueID int, percentage int)
	UpdateProgressWithStage(queueID int, percentage int, stage string)
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
	stage       string
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

// WithStage sets a human-readable stage label that is attached to every progress update
// emitted by this tracker. Returns the same tracker for chaining.
func (pt *Tracker) WithStage(stage string) *Tracker {
	if pt != nil {
		pt.stage = stage
	}
	return pt
}

// Slice returns a child tracker covering segment idx of count equal slices of
// this tracker's [min,max] range. Useful for dividing a range across a known
// number of sequential sub-operations (e.g. one slice per ISO across a
// multi-disc group). Safe on a nil receiver (returns nil).
func (pt *Tracker) Slice(idx, count int) *Tracker {
	if pt == nil || count <= 0 {
		return nil
	}
	span := pt.maxPercent - pt.minPercent
	return &Tracker{
		queueID:     pt.queueID,
		broadcaster: pt.broadcaster,
		minPercent:  pt.minPercent + idx*span/count,
		maxPercent:  pt.minPercent + (idx+1)*span/count,
		stage:       pt.stage,
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
		pt.broadcaster.UpdateProgressWithStage(pt.queueID, percentage, pt.stage)
	}
}

// UpdateAbsolute reports an absolute percentage value, bypassing the tracker's range.
// The stored stage label is still attached to the broadcast update.
func (pt *Tracker) UpdateAbsolute(percentage int) {
	if pt != nil && pt.broadcaster != nil {
		pt.broadcaster.UpdateProgressWithStage(pt.queueID, percentage, pt.stage)
	}
}
