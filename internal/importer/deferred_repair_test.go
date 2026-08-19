package importer

import (
	"errors"
	"testing"
)

// A damaged archive set with PAR2 files defers for repair instead of being
// dropped, when repair_on_import is on. Archive volumes cannot be zero-filled,
// but PAR2 can reconstruct them exactly — so dropping them is a lost repair.
func TestDeferDecision(t *testing.T) {
	tests := []struct {
		name        string
		enabled     bool
		hasPar2     bool
		brokenInSet bool
		want        bool
	}{
		{"damaged set with par2 and feature on defers", true, true, true, true},
		{"feature off drops as before", false, true, true, false},
		{"no par2 files: nothing to repair with", true, false, true, false},
		{"nothing broken: normal import", true, true, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldDeferForRepair(tt.enabled, tt.hasPar2, tt.brokenInSet)
			if got != tt.want {
				t.Fatalf("shouldDeferForRepair(%v,%v,%v) = %v, want %v",
					tt.enabled, tt.hasPar2, tt.brokenInSet, got, tt.want)
			}
		})
	}
}

// The deferral surfaces as a distinct sentinel so the service can park the
// queue item rather than failing it.
func TestErrDeferredForRepairIsDistinct(t *testing.T) {
	err := ErrDeferredForRepair
	if !errors.Is(err, ErrDeferredForRepair) {
		t.Fatal("sentinel must match itself")
	}
	if errors.Is(err, ErrArticlesNotFound) {
		t.Fatal("deferral must not be mistaken for a normal missing-articles failure")
	}
}
