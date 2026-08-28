package config

import (
	"testing"
	"time"
)

func TestGetImportVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetImportVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Import.VerifyContent = &enabled
	if !c.GetImportVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetImportVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
	secs := 5
	c.Import.VerifyContentTimeoutSeconds = &secs
	if got := c.GetImportVerifyContentTimeout(); got != 5*time.Second {
		t.Errorf("got %v, want 5s", got)
	}
}

func TestGetHealthVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetHealthVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Health.VerifyContent = &enabled
	if !c.GetHealthVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetHealthVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
}

func TestDefaultConfig_VerifyContentDisabledByDefault(t *testing.T) {
	c := DefaultConfig()
	if c.GetImportVerifyContent() {
		t.Error("expected import verify_content to default to false")
	}
	if c.GetHealthVerifyContent() {
		t.Error("expected health verify_content to default to false")
	}
}

func TestDefaultConfig_VerifyContentTimeoutDefaultsTo15Seconds(t *testing.T) {
	c := DefaultConfig()
	if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("import: got %v, want 15s default", got)
	}
	if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("health: got %v, want 15s default", got)
	}
}

func TestGetImportVerifyContentTimeout_NonPositiveFallsBackTo15Seconds(t *testing.T) {
	for _, secs := range []int{0, -1, -30} {
		c := &Config{}
		c.Import.VerifyContentTimeoutSeconds = &secs
		if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
			t.Errorf("secs=%d: got %v, want 15s fallback", secs, got)
		}
	}
}

func TestGetHealthVerifyContentTimeout_NonPositiveFallsBackTo15Seconds(t *testing.T) {
	for _, secs := range []int{0, -1, -30} {
		c := &Config{}
		c.Health.VerifyContentTimeoutSeconds = &secs
		if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
			t.Errorf("secs=%d: got %v, want 15s fallback", secs, got)
		}
	}
}

func TestValidate_ImportVerifyContentTimeoutSeconds(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Health.Enabled = nil
		return c
	}

	zero := 0
	negative := -5
	positive := 10

	tests := []struct {
		name    string
		secs    *int
		wantErr bool
	}{
		{"nil is valid (falls back to default)", nil, false},
		{"positive is valid", &positive, false},
		{"zero is invalid", &zero, true},
		{"negative is invalid", &negative, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			c.Import.VerifyContentTimeoutSeconds = tt.secs
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no validation error, got %v", err)
			}
		})
	}
}

func TestValidate_HealthVerifyContentTimeoutSeconds(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Health.Enabled = nil
		return c
	}

	zero := 0
	negative := -5
	positive := 10

	tests := []struct {
		name    string
		secs    *int
		wantErr bool
	}{
		{"nil is valid (falls back to default)", nil, false},
		{"positive is valid", &positive, false},
		{"zero is invalid", &zero, true},
		{"negative is invalid", &negative, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			c.Health.VerifyContentTimeoutSeconds = tt.secs
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no validation error, got %v", err)
			}
		})
	}
}
