package postprocessor

import (
	"testing"

	"github.com/javi11/altmount/internal/database"
)

func TestShouldSkipPostImportLinks(t *testing.T) {
	tests := []struct {
		name string
		item *database.ImportQueueItem
		want bool
	}{
		{"nil item → do not skip", nil, false},
		{"flag false → do not skip", &database.ImportQueueItem{SkipPostImportLinks: false}, false},
		{"flag true → skip", &database.ImportQueueItem{SkipPostImportLinks: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldSkipPostImportLinks(tt.item); got != tt.want {
				t.Errorf("shouldSkipPostImportLinks() = %v, want %v", got, tt.want)
			}
		})
	}
}
