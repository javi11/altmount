package config

import "testing"

func TestMigrateGlobalUserAgent(t *testing.T) {
	t.Run("adopts legacy nzblnk user agent when global is default", func(t *testing.T) {
		c := DefaultConfig()
		c.Nzblnk.UserAgent = "LegacyAgent/1.0"
		migrateGlobalUserAgent(c)
		if c.UserAgent != "LegacyAgent/1.0" {
			t.Fatalf("expected legacy agent adopted, got %q", c.UserAgent)
		}
		if c.Nzblnk.UserAgent != "" {
			t.Fatal("expected legacy field cleared")
		}
	})

	t.Run("keeps explicit global user agent", func(t *testing.T) {
		c := DefaultConfig()
		c.UserAgent = "Explicit/2.0"
		c.Nzblnk.UserAgent = "LegacyAgent/1.0"
		migrateGlobalUserAgent(c)
		if c.UserAgent != "Explicit/2.0" {
			t.Fatalf("expected explicit agent kept, got %q", c.UserAgent)
		}
		if c.Nzblnk.UserAgent != "" {
			t.Fatal("expected legacy field cleared")
		}
	})

	t.Run("noop without legacy value", func(t *testing.T) {
		c := DefaultConfig()
		migrateGlobalUserAgent(c)
		if c.UserAgent != DefaultUserAgent {
			t.Fatalf("expected default agent, got %q", c.UserAgent)
		}
	})
}

func TestGetUserAgentFallback(t *testing.T) {
	if got := (&Config{}).GetUserAgent(); got != DefaultUserAgent {
		t.Fatalf("expected DefaultUserAgent fallback, got %q", got)
	}
	c := &Config{UserAgent: "Custom/1.0"}
	if got := c.GetUserAgent(); got != "Custom/1.0" {
		t.Fatalf("expected configured agent, got %q", got)
	}
}
