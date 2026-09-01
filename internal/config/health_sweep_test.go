package config

import "testing"

// The knob bounds STAT concurrency, not connections. STAT is bodyless and
// pipelines many per connection, so sizing a sweep by connection count is the
// wrong unit — the import path already made this correction; health did not.
func TestMaxConcurrentSegmentChecks_ZeroMeansAdapt(t *testing.T) {
	c := &Config{}
	if got := c.GetMaxConcurrentSegmentChecks(); got != 0 {
		t.Errorf("unset must mean adapt (0), got %d", got)
	}

	n := 250
	c = &Config{}
	c.Health.MaxConcurrentSegmentChecks = &n
	if got := c.GetMaxConcurrentSegmentChecks(); got != 250 {
		t.Errorf("explicit 250: got %d", got)
	}

	zero := 0
	c = &Config{}
	c.Health.MaxConcurrentSegmentChecks = &zero
	if got := c.GetMaxConcurrentSegmentChecks(); got != 0 {
		t.Errorf("explicit 0 must mean adapt, got %d", got)
	}
}

// A config written before the rename must keep working, and the legacy key must
// be cleared so it drops out of saved YAML.
func TestMigrateHealthSweepConcurrency(t *testing.T) {
	c := &Config{}
	c.Health.MaxConnectionsForHealthChecks = 100
	migrateHealthSweepConcurrency(c)

	if c.Health.MaxConcurrentSegmentChecks == nil {
		t.Fatal("legacy value was not carried over")
	}
	if got := *c.Health.MaxConcurrentSegmentChecks; got != 100 {
		t.Errorf("carried over %d, want 100", got)
	}
	if c.Health.MaxConnectionsForHealthChecks != 0 {
		t.Errorf("legacy field not cleared: %d", c.Health.MaxConnectionsForHealthChecks)
	}
}

func TestMigrateHealthSweepConcurrency_IsIdempotent(t *testing.T) {
	c := &Config{}
	c.Health.MaxConnectionsForHealthChecks = 42
	migrateHealthSweepConcurrency(c)
	first := *c.Health.MaxConcurrentSegmentChecks
	migrateHealthSweepConcurrency(c)
	if *c.Health.MaxConcurrentSegmentChecks != first {
		t.Errorf("second run changed the value: %d -> %d", first, *c.Health.MaxConcurrentSegmentChecks)
	}
}

// An explicit new-key setting must win over a stale legacy key.
func TestMigrateHealthSweepConcurrency_DoesNotClobberExplicit(t *testing.T) {
	n := 7
	c := &Config{}
	c.Health.MaxConcurrentSegmentChecks = &n
	c.Health.MaxConnectionsForHealthChecks = 100
	migrateHealthSweepConcurrency(c)

	if *c.Health.MaxConcurrentSegmentChecks != 7 {
		t.Errorf("legacy key clobbered an explicit setting: got %d, want 7",
			*c.Health.MaxConcurrentSegmentChecks)
	}
	if c.Health.MaxConnectionsForHealthChecks != 0 {
		t.Error("legacy field should still be cleared")
	}
}

// The default must reach the adaptive path, which the old default of 100
// (with validation rejecting <= 0) made unreachable in every valid config.
func TestDefaultConfigReachesAdaptiveSweep(t *testing.T) {
	c := DefaultConfig()
	if got := c.GetMaxConcurrentSegmentChecks(); got != 0 {
		t.Errorf("default must be adaptive (0), got %d — the stream-aware branch would be dead code", got)
	}
	if err := c.Validate(); err != nil {
		t.Errorf("default config must validate: %v", err)
	}
}
