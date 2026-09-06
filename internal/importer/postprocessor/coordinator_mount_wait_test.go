package postprocessor

import (
	"testing"

	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/database"
)

type fakeInstanceLister struct{ instances []*model.ConfigInstance }

func (f fakeInstanceLister) GetAllInstances() []*model.ConfigInstance { return f.instances }

func TestShouldWaitForMountPropagation(t *testing.T) {
	cat := "movies"
	target := "/mnt/target/file.mkv"
	one := []*model.ConfigInstance{{Name: "radarr"}}

	tests := []struct {
		name   string
		lister arrInstanceLister
		item   *database.ImportQueueItem
		want   bool
	}{
		{"no arr service", nil, &database.ImportQueueItem{Category: &cat}, false},
		{"arr service with no instances", fakeInstanceLister{}, &database.ImportQueueItem{Category: &cat}, false},
		{"instances but item has no category or target", fakeInstanceLister{one}, &database.ImportQueueItem{}, false},
		{"instances and category", fakeInstanceLister{one}, &database.ImportQueueItem{Category: &cat}, true},
		{"instances and target path", fakeInstanceLister{one}, &database.ImportQueueItem{TargetPath: &target}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldWaitForMountPropagation(tt.lister, tt.item); got != tt.want {
				t.Errorf("shouldWaitForMountPropagation() = %v, want %v", got, tt.want)
			}
		})
	}
}
