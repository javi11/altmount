package postprocessor

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
)

// TestSkipARRNotificationFromMetadata verifies the metadata decode logic in isolation.
func TestSkipARRNotificationFromMetadata(t *testing.T) {
	otherMeta := `{"nzbdav_id":"abc123"}`
	otherMetaWithFlag := `{"nzbdav_id":"abc123","skip_arr_notification":true}`

	tests := []struct {
		name     string
		metadata *string
		want     bool
	}{
		{
			name:     "nil metadata → do not skip",
			metadata: nil,
			want:     false,
		},
		{
			name:     "empty string → do not skip",
			metadata: new(string),
			want:     false,
		},
		{
			name:     "flag false → do not skip",
			metadata: jsonMeta(database.ImportQueueMetadata{SkipARRNotification: false}),
			want:     false,
		},
		{
			name:     "flag true → skip",
			metadata: jsonMeta(database.ImportQueueMetadata{SkipARRNotification: true}),
			want:     true,
		},
		{
			name:     "other metadata fields, no flag → do not skip",
			metadata: &otherMeta,
			want:     false,
		},
		{
			name:     "other metadata fields + flag true → skip",
			metadata: &otherMetaWithFlag,
			want:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &database.ImportQueueItem{Metadata: tt.metadata}
			got := shouldSkipARRNotification(item)
			if got != tt.want {
				t.Errorf("shouldSkipARRNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func jsonMeta(v any) *string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	s := string(b)
	return &s
}

// TestCoordinator_HandleSuccess_SkipsARRNotification verifies that HandleSuccess
// runs without error and leaves ARRNotified=false when skip_arr_notification is set.
//
// Note: arrs.Service requires concrete DB dependencies so it is left nil here.
// Because notifyARRWith guards on nil arrsService, ARRNotified would be false
// regardless of the skip check. Call-site regression safety (i.e. that the
// shouldSkipARRNotification guard is actually reached) is covered by
// TestSkipARRNotificationFromMetadata, which tests the helper directly.
//
// This test runs in ~1s due to the FUSE mount propagation delay in HandleSuccess.
func TestCoordinator_HandleSuccess_SkipsARRNotification(t *testing.T) {
	meta := `{"skip_arr_notification":true}`
	item := &database.ImportQueueItem{
		ID:       1,
		Metadata: &meta,
	}

	// MountTypeNone → notifyVFSWith returns early.
	// ImportStrategy "" → CreateSymlinks and CreateStrmFiles return early.
	cfg := &config.Config{MountType: config.MountTypeNone}
	configGetter := func() *config.Config { return cfg }

	// t.TempDir() gives a real directory so metadata walks succeed without panic.
	// HealthRepo nil → ScheduleHealthCheck is skipped.
	metaSvc := metadata.NewMetadataService(t.TempDir())

	coord := NewCoordinator(Config{
		ConfigGetter:    configGetter,
		MetadataService: metaSvc,
	})

	result, err := coord.HandleSuccess(context.Background(), item, "/some/path")
	if err != nil {
		t.Fatalf("HandleSuccess returned unexpected error: %v", err)
	}
	if result.ARRNotified {
		t.Error("expected ARRNotified to be false when skip_arr_notification is set, got true")
	}
}

