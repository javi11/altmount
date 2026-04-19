package postprocessor

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/metadata"
)

func TestSkipARRNotificationFromField(t *testing.T) {
	tests := []struct {
		name string
		flag bool
		want bool
	}{
		{"false → do not skip", false, false},
		{"true → skip", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := &database.ImportQueueItem{SkipArrNotification: tt.flag}
			if got := shouldSkipARRNotification(item); got != tt.want {
				t.Errorf("shouldSkipARRNotification() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCoordinator_HandleSuccess_SkipsARRNotification(t *testing.T) {
	item := &database.ImportQueueItem{
		ID:                  1,
		SkipArrNotification: true,
	}

	cfg := &config.Config{MountType: config.MountTypeNone}
	configGetter := func() *config.Config { return cfg }
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
		t.Error("expected ARRNotified to be false when SkipArrNotification is set, got true")
	}
}
