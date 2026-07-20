package config

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/viper"
)

// captureWarn runs fn with slog routed to a buffer and returns what was logged.
func captureWarn(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestWarnUnknownConfigKeys_ReportsRetiredKey(t *testing.T) {
	path := writeConfig(t, `
import:
  max_processor_workers: 4
  max_import_connections: 60
`)
	viper.Reset()
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	out := captureWarn(t, warnUnknownConfigKeys)
	if !strings.Contains(out, "max_import_connections") {
		t.Errorf("warning did not name the retired key; got: %s", out)
	}
}

func TestWarnUnknownConfigKeys_SilentOnValidConfig(t *testing.T) {
	path := writeConfig(t, `
import:
  max_processor_workers: 4
  segment_sample_percentage: 25
`)
	viper.Reset()
	viper.SetConfigFile(path)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("read config: %v", err)
	}

	if out := captureWarn(t, warnUnknownConfigKeys); out != "" {
		t.Errorf("expected no warning for a valid config, got: %s", out)
	}
}
