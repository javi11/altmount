package api

import (
	"encoding/json"
	"testing"

	"github.com/javi11/altmount/internal/database"
)

func TestManualImportRequest_SkipArrNotificationEncoding(t *testing.T) {
	req := ManualImportRequest{
		SkipArrNotification: true,
	}

	// Simulate what the handler does: encode into metadata when flag is true
	type importMeta struct {
		SkipARRNotification bool `json:"skip_arr_notification"`
	}

	var item database.ImportQueueItem
	if req.SkipArrNotification {
		b, err := json.Marshal(importMeta{SkipARRNotification: true})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		s := string(b)
		item.Metadata = &s
	}

	if item.Metadata == nil {
		t.Fatal("expected Metadata to be set")
	}

	var got importMeta
	if err := json.Unmarshal([]byte(*item.Metadata), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.SkipARRNotification {
		t.Error("expected skip_arr_notification to be true")
	}
}

func TestManualImportRequest_SkipArrNotification_FalseByDefault(t *testing.T) {
	req := ManualImportRequest{}

	var item database.ImportQueueItem
	if req.SkipArrNotification {
		b, _ := json.Marshal(struct {
			SkipARRNotification bool `json:"skip_arr_notification"`
		}{SkipARRNotification: true})
		s := string(b)
		item.Metadata = &s
	}

	if item.Metadata != nil {
		t.Error("expected Metadata to be nil when SkipArrNotification is false")
	}
}
