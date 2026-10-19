package slogutil

import (
	"bytes"
	"errors"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
)

const testKey = "29a51a03b108e3986cc08b3a2d6f0fc9b4729349b09968c014450560cd2585f6"

func TestRedactSecrets(t *testing.T) {
	tests := []struct{ in, want string }{
		{"http://h:3030/api/files/stream?path=a.mkv&download_key=" + testKey, "http://h:3030/api/files/stream?path=a.mkv&download_key=REDACTED"},
		{"https://indexer/api?t=get&apikey=abc123&id=9", "https://indexer/api?t=get&apikey=REDACTED&id=9"},
		{"https://indexer/dl?API_KEY=abc123", "https://indexer/dl?API_KEY=REDACTED"},
		{"no secrets here?path=a&b=c", "no secrets here?path=a&b=c"},
	}
	for _, tt := range tests {
		if got := RedactSecrets(tt.in); got != tt.want {
			t.Errorf("RedactSecrets(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// The key must not reach either log output, whether it is in the message,
// an attribute, an error, or a URL value.
func TestLogHandlersRedactSecrets(t *testing.T) {
	streamURL := "http://192.168.0.10:3030/api/files/stream?path=a.mkv&download_key=" + testKey
	u, _ := url.Parse(streamURL)
	for name, newHandler := range map[string]func(*bytes.Buffer) slog.Handler{
		"text": func(b *bytes.Buffer) slog.Handler {
			return slog.NewTextHandler(b, &slog.HandlerOptions{ReplaceAttr: redactAttr})
		},
		"json": func(b *bytes.Buffer) slog.Handler {
			return slog.NewJSONHandler(b, &slog.HandlerOptions{ReplaceAttr: redactAttr})
		},
	} {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(newHandler(&buf))
			log.Info("Returning local library Stremio stream", "library_url", streamURL)
			log.Info("redirect to "+streamURL, "url", u)
			log.Error("stream failed", "error", errors.New(`Get "`+streamURL+`": EOF`))
			log.WithGroup("req").Info("grouped", "library_url", streamURL)

			out := buf.String()
			if strings.Contains(out, testKey) {
				t.Fatalf("download key leaked into the log:\n%s", out)
			}
			// The second line carries the URL twice: in the message and an attribute.
			if n := strings.Count(out, "download_key=REDACTED"); n != 5 {
				t.Errorf("want 5 redactions, got %d:\n%s", n, out)
			}
		})
	}
}

// The logger AltMount actually uses writes no download key to its log file.
func TestSetupLogRotationRedactsSecrets(t *testing.T) {
	logFile := filepath.Join(t.TempDir(), "altmount.log")
	logger, _ := SetupLogRotation(config.LogConfig{File: logFile, MaxSize: 1})
	logger.Info("Returning local library Stremio stream",
		"library_url", "http://h:3030/api/files/stream?path=a.mkv&download_key="+testKey)

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), testKey) {
		t.Fatalf("download key written to the log file:\n%s", data)
	}
	if !strings.Contains(string(data), "download_key=REDACTED") {
		t.Errorf("expected a redacted URL in the log file:\n%s", data)
	}
}
