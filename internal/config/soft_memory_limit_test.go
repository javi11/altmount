package config

import "testing"

func memLimitConfig(cacheMB int, par2MB, par2Jobs int) *Config {
	c := DefaultConfig()
	c.SegmentCache.MemoryMB = &cacheMB
	c.Par2Repair.MaxMemoryMB = par2MB
	c.Par2Repair.MaxConcurrentJobs = par2Jobs
	enabled := true
	c.Par2Repair.Enabled = &enabled
	c.Providers = nil
	return c
}

// headroomBytes is the expected headroom for a config with no providers:
// runtime base plus the budgeted read-ahead windows.
func headroomBytes() int64 {
	return int64(softMemoryBaseMB)<<20 + softMemoryStreams*StreamReadAheadBytesCap
}

func withProviders(c *Config, conns ...int) *Config {
	on := true
	for _, n := range conns {
		c.Providers = append(c.Providers, ProviderConfig{Host: "h", Port: 563, MaxConnections: n, Enabled: &on})
	}
	return c
}

func TestSoftMemoryLimitHeadroomScalesWithReadAheadAndConnections(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	disabled := false
	c.Par2Repair.Enabled = &disabled
	base := c.SoftMemoryLimit("")
	if want := int64(256)<<20 + headroomBytes(); base != want {
		t.Fatalf("SoftMemoryLimit without providers = %d, want %d", base, want)
	}
	// The user config that triggered a GC spiral: 341 connections across
	// six providers and a 256 MB memory tier had a 512 MiB limit.
	withProviders(c, 48, 80, 55, 55, 48, 55)
	got := c.SoftMemoryLimit("")
	if want := base + 341*softMemoryPerConnectionBytes; got != want {
		t.Fatalf("SoftMemoryLimit with 341 connections = %d, want %d", got, want)
	}
	if got <= int64(700)<<20 {
		t.Fatalf("SoftMemoryLimit with 341 connections = %d MB, must exceed the ~700 MB live set", got>>20)
	}
}

func TestSoftMemoryLimitCountsBackupAndSkipsDisabledProviders(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	base := c.SoftMemoryLimit("")
	on, off := true, false
	c.Providers = []ProviderConfig{
		{Host: "a", Port: 563, MaxConnections: 10, Enabled: &on},
		{Host: "b", Port: 563, MaxConnections: 20, Enabled: &on, IsBackupProvider: &on},
		{Host: "c", Port: 563, MaxConnections: 99, Enabled: &off},
	}
	if got, want := c.SoftMemoryLimit(""), base+30*softMemoryPerConnectionBytes; got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d (backup counted, disabled skipped)", got, want)
	}
}

func TestSoftMemoryLimitIgnoresPar2BudgetWhenRepairDisabled(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	disabled := false
	c.Par2Repair.Enabled = &disabled
	want := int64(256)<<20 + headroomBytes()
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit with repair disabled = %d, want %d", got, want)
	}
	c.Par2Repair.Enabled = nil
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit with repair unset = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitAutoAddsCachePar2AndHeadroom(t *testing.T) {
	c := memLimitConfig(256, 256, 1)
	want := int64(256+256)<<20 + headroomBytes()
	if got := c.SoftMemoryLimit(""); got != want {
		t.Fatalf("SoftMemoryLimit = %d, want %d", got, want)
	}
}

func TestSoftMemoryLimitAutoScalesPar2ByConcurrentJobs(t *testing.T) {
	c := memLimitConfig(128, 100, 3)
	want := int64(128+300)<<20 + headroomBytes()
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
