package config

import "testing"

func TestDefaultConfig_MetadataMigrationDefaultGroup(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.Metadata.Migration.DefaultGroup != "alt.binaries.misc" {
		t.Fatalf("expected default group alt.binaries.misc, got %q", cfg.Metadata.Migration.DefaultGroup)
	}
}
