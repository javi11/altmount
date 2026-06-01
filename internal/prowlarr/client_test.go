package prowlarr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/javi11/altmount/internal/sabnzbd"
)

// TestDownloadNZBSendsSABnzbdUserAgent verifies that NZB downloads identify as a
// SABnzbd client on the wire, alongside the Prowlarr API key. This matters for
// indexers configured to hand back direct/redirect download links, which AltMount
// then fetches itself.
func TestDownloadNZBSendsSABnzbdUserAgent(t *testing.T) {
	var gotUA, gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotKey = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/x-nzb")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><nzb></nzb>`))
	}))
	defer srv.Close()

	client := NewClient(srv.URL, "test-key", srv.Client())

	body, err := client.DownloadNZB(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("DownloadNZB returned error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected NZB body, got empty")
	}

	if want := sabnzbd.SABnzbdUserAgent(); gotUA != want {
		t.Fatalf("User-Agent = %q, want %q", gotUA, want)
	}
	if gotKey != "test-key" {
		t.Fatalf("X-Api-Key = %q, want %q", gotKey, "test-key")
	}
}

// TestDownloadNZBPreservesUserAgentAcrossRedirect verifies the SABnzbd identity
// survives a Prowlarr "redirect" download (302 → indexer), which most indexers
// require. Prowlarr returns the real indexer URL in a redirect; the HTTP client
// follows it and must still present SABnzbd/<version> to the indexer.
func TestDownloadNZBPreservesUserAgentAcrossRedirect(t *testing.T) {
	var indexerUA string
	indexerHit := false
	// Final destination: the actual indexer Prowlarr redirects to.
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		indexerHit = true
		indexerUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/x-nzb")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><nzb></nzb>`))
	}))
	defer indexer.Close()

	// Prowlarr: responds with a 302 redirect to the indexer, as it does when an
	// indexer is configured with redirect download.
	prowlarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, indexer.URL+"/getnzb/abc", http.StatusFound)
	}))
	defer prowlarr.Close()

	client := NewClient(prowlarr.URL, "test-key", prowlarr.Client())

	body, err := client.DownloadNZB(context.Background(), prowlarr.URL+"/download")
	if err != nil {
		t.Fatalf("DownloadNZB returned error: %v", err)
	}
	if len(body) == 0 {
		t.Fatal("expected NZB body, got empty")
	}
	if !indexerHit {
		t.Fatal("redirect was not followed to the indexer")
	}
	if want := sabnzbd.SABnzbdUserAgent(); indexerUA != want {
		t.Fatalf("indexer (post-redirect) saw User-Agent %q, want %q", indexerUA, want)
	}
}
