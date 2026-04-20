package api

import "testing"

func TestManualImportRequest_SkipArrNotification_True(t *testing.T) {
	req := ManualImportRequest{SkipArrNotification: true}
	if !req.SkipArrNotification {
		t.Error("expected SkipArrNotification to be true")
	}
}

func TestManualImportRequest_SkipArrNotification_FalseByDefault(t *testing.T) {
	req := ManualImportRequest{}
	if req.SkipArrNotification {
		t.Error("expected SkipArrNotification to be false by default")
	}
}
