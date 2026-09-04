package config

import "testing"

func TestSoftMemoryLimitDerivesFromCacheBudget(t *testing.T) {
	mb := 256
	c := SegmentCacheConfig{MemoryMB: &mb}
	want := int64(256+softMemoryHeadroomMB) << 20
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitUsesDefaultBudgetWhenUnset(t *testing.T) {
	c := SegmentCacheConfig{}
	want := int64(defaultSegmentCacheMemoryMB+softMemoryHeadroomMB) << 20
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitDefersToOperatorGOMEMLIMIT(t *testing.T) {
	mb := 256
	c := SegmentCacheConfig{MemoryMB: &mb}
	if got := c.SoftMemoryLimit("2GiB"); got != 0 {
		t.Fatalf("SoftMemoryLimit with GOMEMLIMIT set = %d, want 0", got)
	}
}

func TestSoftMemoryLimitOffWhenMemoryTierDisabled(t *testing.T) {
	zero := 0
	c := SegmentCacheConfig{MemoryMB: &zero}
	if got := c.SoftMemoryLimit(""); got != 0 {
		t.Fatalf("SoftMemoryLimit with memory tier off = %d, want 0", got)
	}
}

func TestValidate_SegmentCacheMemoryMB(t *testing.T) {
	zero, negative, big := 0, -1, 4096
	tests := []struct {
		name    string
		mb      *int
		wantErr bool
	}{
		{"nil defaults", nil, false},
		{"zero disables", &zero, false},
		{"large is valid", &big, false},
		{"negative is invalid", &negative, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := DefaultConfig()
			c.Health.Enabled = nil
			c.SegmentCache.MemoryMB = tt.mb
			err := c.Validate()
			if tt.wantErr != (err != nil) {
				t.Fatalf("wantErr=%v, got %v", tt.wantErr, err)
			}
		})
	}
}
