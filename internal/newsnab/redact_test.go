package newsnab

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

const leakCanaryKey = "SUPERSECRETKEY123"

// TestClientErrorsNeverContainAPIKey pins that no error returned by this client
// carries the indexer API key. Every request embeds the key as a query
// parameter, and http.Client returns a *url.Error whose message is the full
// URL — so an unredacted error puts the credential wherever the caller logs or
// renders it. The search coordinator logs these at WARN and the indexer-test
// endpoint returns them in an HTTP response body.
func TestClientErrorsNeverContainAPIKey(t *testing.T) {
	// Port 1 is guaranteed to refuse, so every call fails at the transport
	// layer — the exact path that produces a *url.Error.
	c := NewClient(IndexerConfig{
		Name:    "idx",
		URL:     "http://127.0.0.1:1/",
		APIKey:  leakCanaryKey,
		Enabled: true,
	}, &http.Client{})
	ctx := context.Background()

	cases := []struct {
		name string
		call func() error
	}{
		{"CheckCaps", func() error { _, err := c.CheckCaps(ctx, "ua"); return err }},
		{"SearchMovie", func() error { _, err := c.SearchMovie(ctx, "tt123", "Title", nil, "ua"); return err }},
		{"SearchTV", func() error { _, err := c.SearchTV(ctx, "tt123", "", "Title", 1, 1, nil, "ua"); return err }},
		{"SearchGeneral", func() error { _, err := c.SearchGeneral(ctx, "Title", nil, "ua"); return err }},
		{"DownloadNZB", func() error {
			_, err := c.DownloadNZB(ctx, "http://127.0.0.1:1/get?id=1&apikey="+leakCanaryKey, "ua")
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected a transport error, got nil")
			}
			if strings.Contains(err.Error(), leakCanaryKey) {
				t.Errorf("error exposes the API key: %v", err)
			}
			// The error must still identify what failed.
			if !strings.Contains(err.Error(), "newsnab") {
				t.Errorf("error lost its context: %v", err)
			}
		})
	}
}

// TestSearchErrorStripsQueryAndUserinfo covers the end-to-end redaction of a
// URL carrying both an apikey and userinfo.
func TestSearchErrorStripsQueryAndUserinfo(t *testing.T) {
	t.Run("query and userinfo are stripped", func(t *testing.T) {
		c := NewClient(IndexerConfig{
			Name: "idx", URL: "http://user:pw@127.0.0.1:1/", APIKey: leakCanaryKey, Enabled: true,
		}, &http.Client{})
		_, err := c.SearchGeneral(context.Background(), "x", nil, "ua")
		if err == nil {
			t.Fatal("expected a transport error")
		}
		msg := err.Error()
		for _, secret := range []string{leakCanaryKey, "pw"} {
			if strings.Contains(msg, secret) {
				t.Errorf("error exposes %q: %v", secret, err)
			}
		}
		if !strings.Contains(msg, "127.0.0.1:1") {
			t.Errorf("error should keep the host for diagnostics: %v", err)
		}
	})
}
