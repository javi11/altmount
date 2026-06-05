package progress

import "testing"

// recordingBroadcaster captures every progress update for assertions.
type recordingBroadcaster struct {
	updates []recordedUpdate
}

type recordedUpdate struct {
	queueID    int
	percentage int
	stage      string
}

func (rb *recordingBroadcaster) UpdateProgress(queueID, percentage int) {
	rb.updates = append(rb.updates, recordedUpdate{queueID: queueID, percentage: percentage})
}

func (rb *recordingBroadcaster) UpdateProgressWithStage(queueID, percentage int, stage string) {
	rb.updates = append(rb.updates, recordedUpdate{queueID: queueID, percentage: percentage, stage: stage})
}

func TestTrackerSlice(t *testing.T) {
	t.Parallel()

	rb := &recordingBroadcaster{}
	base := NewTracker(rb, 7, 10, 30).WithStage("Analyzing ISO")

	tests := []struct {
		name             string
		idx, count       int
		wantMin, wantMax int
		wantNil          bool
	}{
		{name: "first of two", idx: 0, count: 2, wantMin: 10, wantMax: 20},
		{name: "second of two", idx: 1, count: 2, wantMin: 20, wantMax: 30},
		{name: "single slice is full range", idx: 0, count: 1, wantMin: 10, wantMax: 30},
		{name: "zero count is nil", idx: 0, count: 0, wantNil: true},
		{name: "negative count is nil", idx: 0, count: -3, wantNil: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := base.Slice(tt.idx, tt.count)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("Slice(%d,%d) = %+v, want nil", tt.idx, tt.count, got)
				}
				return
			}
			if got == nil {
				t.Fatalf("Slice(%d,%d) = nil, want non-nil", tt.idx, tt.count)
			}
			if got.minPercent != tt.wantMin || got.maxPercent != tt.wantMax {
				t.Errorf("Slice(%d,%d) range = [%d,%d], want [%d,%d]",
					tt.idx, tt.count, got.minPercent, got.maxPercent, tt.wantMin, tt.wantMax)
			}
			// Child inherits queueID, broadcaster, and stage.
			if got.queueID != base.queueID || got.broadcaster != base.broadcaster || got.stage != base.stage {
				t.Errorf("Slice did not inherit parent identity/stage: %+v", got)
			}
		})
	}
}

func TestTrackerSliceNilReceiver(t *testing.T) {
	t.Parallel()

	var nilTracker *Tracker
	if got := nilTracker.Slice(0, 2); got != nil {
		t.Fatalf("nil receiver Slice = %+v, want nil", got)
	}
	// And Update on the nil result must be a safe no-op.
	nilTracker.Slice(0, 2).Update(1, 2)
}

func TestTrackerSliceUpdateMapsIntoSubRange(t *testing.T) {
	t.Parallel()

	rb := &recordingBroadcaster{}
	base := NewTracker(rb, 7, 10, 30).WithStage("Analyzing ISO")

	// Second of two ISOs spans [20,30]; a half-complete update lands at 25.
	base.Slice(1, 2).Update(1, 2)

	if len(rb.updates) != 1 {
		t.Fatalf("got %d updates, want 1", len(rb.updates))
	}
	u := rb.updates[0]
	if u.queueID != 7 || u.percentage != 25 || u.stage != "Analyzing ISO" {
		t.Fatalf("update = %+v, want {7 25 Analyzing ISO}", u)
	}
}
