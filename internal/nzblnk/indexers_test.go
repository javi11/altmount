package nzblnk

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/sabnzbd"
)

// capturingRT records the User-Agent of every request and returns a canned
// response, so indexer requests can be verified without real network access.
type capturingRT struct {
	ua   string
	body string
}

func (c *capturingRT) RoundTrip(r *http.Request) (*http.Response, error) {
	c.ua = r.Header.Get("User-Agent")
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"application/x-nzb"}},
		Body:       io.NopCloser(strings.NewReader(c.body)),
		Request:    r,
	}, nil
}

// TestIndexersSendConfiguredUserAgent verifies that each public-indexer client
// stamps the supplied (default SABnzbd) User-Agent onto its outbound requests.
func TestIndexersSendConfiguredUserAgent(t *testing.T) {
	ua := sabnzbd.SABnzbdUserAgent()

	t.Run("nzbking search", func(t *testing.T) {
		rt := &capturingRT{body: `<a href="/details:deadbeef/">result</a>`}
		idx := NewNZBKingIndexer(&http.Client{Transport: rt}, ua)
		if _, err := idx.Search(context.Background(), "header.pattern"); err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if rt.ua != ua {
			t.Fatalf("nzbking User-Agent = %q, want %q", rt.ua, ua)
		}
	})

	t.Run("nzbindex search", func(t *testing.T) {
		rt := &capturingRT{body: `<a href="/download/12345">result</a>`}
		idx := NewNZBIndexIndexer(&http.Client{Transport: rt}, ua)
		if _, err := idx.Search(context.Background(), "header.pattern"); err != nil {
			t.Fatalf("Search error: %v", err)
		}
		if rt.ua != ua {
			t.Fatalf("nzbindex User-Agent = %q, want %q", rt.ua, ua)
		}
	})

	t.Run("nzbking download", func(t *testing.T) {
		rt := &capturingRT{body: `<?xml version="1.0"?><nzb></nzb>`}
		idx := NewNZBKingIndexer(&http.Client{Transport: rt}, ua)
		if _, err := idx.DownloadNZB(context.Background(), "deadbeef"); err != nil {
			t.Fatalf("DownloadNZB error: %v", err)
		}
		if rt.ua != ua {
			t.Fatalf("nzbking download User-Agent = %q, want %q", rt.ua, ua)
		}
	})
}
