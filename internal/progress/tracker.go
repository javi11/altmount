package progress

// Broadcaster interface for updating progress
type Broadcaster interface {
	UpdateProgress(queueID int, percentage int)
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

// Update reports progress within the configured percentage range
func (pt *Tracker) Update(current, total int) {
	if total > 0 && pt.broadcaster != nil {
		rangeSize := pt.maxPercent - pt.minPercent
		percentage := pt.minPercent + (current * rangeSize / total)
		pt.broadcaster.UpdateProgress(pt.queueID, percentage)
	}
}
