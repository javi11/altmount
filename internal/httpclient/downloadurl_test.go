package httpclient

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

// TestValidateDownloadURL pins which indexer-supplied download targets are
// allowed. Indexer search responses drive these fetches, so a hostile indexer
// must not be able to aim AltMount at loopback, private or link-local
// addresses — cloud instance-metadata endpoints in particular.
func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"public https host", "https://indexer.example.com/getnzb/abc?apikey=k", false},
		{"public http host", "http://indexer.example.com/getnzb/abc", false},
		{"public literal IP", "https://93.184.216.34/getnzb/abc", false},

		{"empty", "", true},
		{"no scheme", "indexer.example.com/getnzb", true},
		{"file scheme", "file:///etc/passwd", true},
		{"gopher scheme", "gopher://example.com/", true},
		{"no host", "http:///getnzb/abc", true},

		{"loopback IPv4", "http://127.0.0.1:8080/getnzb", true},
		{"loopback IPv6", "http://[::1]/getnzb", true},
		{"localhost", "http://localhost:9696/getnzb", true},
		{"localhost subdomain", "http://indexer.localhost/getnzb", true},
		{"unspecified address", "http://0.0.0.0/getnzb", true},
		{"private 10.x", "http://10.0.0.5/getnzb", true},
		{"private 192.168.x", "http://192.168.1.10/getnzb", true},
		{"private 172.16.x", "http://172.16.4.4/getnzb", true},
		{"link-local metadata endpoint", "http://169.254.169.254/latest/meta-data/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDownloadURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateDownloadURL(%q) = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateDownloadURL(%q) = %v, want nil", tt.raw, err)
			}
		})
	}
}

// TestRedactURLError pins that redaction removes credentials from a *url.Error
// while preserving the wrapped cause and the diagnosable parts of the URL.
func TestRedactURLError(t *testing.T) {
	t.Run("nil passes through", func(t *testing.T) {
		if got := RedactURLError(nil); got != nil {
			t.Fatalf("RedactURLError(nil) = %v, want nil", got)
		}
	})

	t.Run("non-url error is returned unchanged", func(t *testing.T) {
		err := errors.New("plain failure")
		if got := RedactURLError(err); got != err {
			t.Fatalf("RedactURLError(%v) = %v, want the same error", err, got)
		}
	})

	t.Run("query, fragment and userinfo are stripped", func(t *testing.T) {
		cause := errors.New("connection refused")
		err := RedactURLError(&url.Error{
			Op:  "Get",
			URL: "https://user:pw@indexer.example.com/api?apikey=SECRET&t=search#frag",
			Err: cause,
		})

		msg := err.Error()
		for _, secret := range []string{"SECRET", "apikey", "user:pw", "frag"} {
			if strings.Contains(msg, secret) {
				t.Errorf("redacted error still contains %q: %v", secret, err)
			}
		}
		if !strings.Contains(msg, "indexer.example.com/api") {
			t.Errorf("redaction dropped the diagnosable host/path: %v", err)
		}
		if !errors.Is(err, cause) {
			t.Errorf("redaction broke the wrapped cause chain")
		}
	})

	t.Run("unparseable url is dropped wholesale", func(t *testing.T) {
		err := RedactURLError(&url.Error{
			Op:  "Get",
			URL: "http://[::1:80/api?apikey=SECRET",
			Err: errors.New("boom"),
		})
		if strings.Contains(err.Error(), "SECRET") {
			t.Errorf("unparseable URL leaked its query: %v", err)
		}
	})
}
