package config

import "testing"

func memLimitConfig(cacheMB int, par2MB, par2Jobs int) *Config {
	c := DefaultConfig()
	c.SegmentCache.MemoryMB = &cacheMB
	c.Par2Repair.MaxMemoryMB = par2MB
	c.Par2Repair.MaxConcurrentJobs = par2Jobs
	return c
}

func TestSoftMemoryLimitAutoAddsCachePar2AndHeadroom(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	want := int64(256+256+softMemoryHeadroomMB) << 20
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitAutoScalesPar2ByConcurrentJobs(t *testing.T) {
	c := memLimitConfig(128, 100, 3)
	want := int64(128+300+softMemoryHeadroomMB) << 20
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitAutoOffWhenMemoryTierDisabled(t *testing.T) {
	c := memLimitConfig(0, 256, 1)
	if got := c.SoftMemoryLimit(""); got != 0 {
		t.Fatalf("SoftMemoryLimit with memory tier off = %d, want 0", got)
	}
}

func TestSoftMemoryLimitExplicitValuePins(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	pinned := 1024
	c.MemoryLimitMB = &pinned
	if got := c.SoftMemoryLimit(""); got != int64(1024)<<20 {
		t.Fatalf("SoftMemoryLimit = %d, want 1 GiB", got)
	}
}

func TestSoftMemoryLimitNegativeDisables(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	off := -1
	c.MemoryLimitMB = &off
	if got := c.SoftMemoryLimit(""); got != 0 {
		t.Fatalf("SoftMemoryLimit = %d, want 0", got)
	}
}

func TestSoftMemoryLimitZeroMeansAuto(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	want := c.SoftMemoryLimit("")
	auto := 0
	c.MemoryLimitMB = &auto
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want auto-derived %d", got, want)
	}
}

func TestSoftMemoryLimitDefersToOperatorGOMEMLIMIT(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	pinned := 1024
	c.MemoryLimitMB = &pinned
	if got := c.SoftMemoryLimit("2GiB"); got != 0 {
		t.Fatalf("SoftMemoryLimit with GOMEMLIMIT set = %d, want 0", got)
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
