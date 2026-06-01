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
