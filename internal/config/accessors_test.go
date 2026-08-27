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
