package prowlarr

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

// TestDownloadNZBErrorNeverContainsAPIKey pins that a failed NZB download does
// not echo the indexer credentials embedded in the download URL. Prowlarr hands
// out download URLs with the upstream indexer's apikey in the query string, and
// net/http reports transport failures as a *url.Error carrying the full URL.
func TestDownloadNZBErrorNeverContainsAPIKey(t *testing.T) {
	const indexerKey = "INDEXERSECRET999"
	c := NewClient("http://127.0.0.1:1", "prowlarr-key", &http.Client{})

	// A reserved .invalid host fails DNS immediately and is not a private
	// address, so it reaches the transport rather than the SSRF guard.
	_, err := c.DownloadNZB(context.Background(),
		"http://indexer.invalid/getnzb/abc?apikey="+indexerKey)
	if err == nil {
		t.Fatal("expected a transport error, got nil")
	}
	if strings.Contains(err.Error(), indexerKey) {
		t.Errorf("error exposes the indexer API key: %v", err)
	}
	if !strings.Contains(err.Error(), "prowlarr") {
		t.Errorf("error lost its context: %v", err)
	}
}
