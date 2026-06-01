package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/sabnzbd"
)

// TestHandleSABnzbdAddUrlSendsSABnzbdUserAgent verifies the addurl path (used by
// Sonarr/Radarr to hand AltMount an indexer NZB URL) fetches that URL while
// identifying as a current SABnzbd, exactly as a real SABnzbd download client
// would. The importer service is intentionally nil: the handler returns a
// graceful SABnzbd error after the download, but the indexer has already
// recorded the User-Agent by then.
func TestHandleSABnzbdAddUrlSendsSABnzbdUserAgent(t *testing.T) {
	var gotUA string
	hit := false
	indexer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/x-nzb")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><nzb></nzb>`))
	}))
	defer indexer.Close()

	s := &Server{
		configManager: &mockConfigManager{cfg: &config.Config{}},
		// importerService left nil on purpose — see doc comment.
	}
	app := fiber.New()
	app.Get("/addurl", s.handleSABnzbdAddUrl)

	req := httptest.NewRequest("GET", "/addurl?name="+url.QueryEscape(indexer.URL+"/file.nzb"), nil)
	resp, err := app.Test(req, -1)
	if err != nil {
		t.Fatalf("app.Test returned error: %v", err)
	}
	defer resp.Body.Close()

	if !hit {
		t.Fatal("indexer was never contacted")
	}
	if want := sabnzbd.SABnzbdUserAgent(); gotUA != want {
		t.Fatalf("indexer saw User-Agent %q, want %q", gotUA, want)
	}
	if !strings.HasPrefix(gotUA, "SABnzbd/") {
		t.Fatalf("User-Agent %q is not a SABnzbd identity", gotUA)
	}
}

// TestNzblnkUserAgentDefault verifies the resolver falls back to the current
// SABnzbd identity when no override is configured, and respects an override.
func TestNzblnkUserAgentDefault(t *testing.T) {
	if got, want := nzblnkUserAgent(""), sabnzbd.SABnzbdUserAgent(); got != want {
		t.Fatalf("nzblnkUserAgent(\"\") = %q, want SABnzbd default %q", got, want)
	}
	if !strings.HasPrefix(nzblnkUserAgent(""), "SABnzbd/") {
		t.Fatalf("default User-Agent is not a SABnzbd identity: %q", nzblnkUserAgent(""))
	}
	if got := nzblnkUserAgent("Custom/1.0"); got != "Custom/1.0" {
		t.Fatalf("nzblnkUserAgent(override) = %q, want Custom/1.0", got)
	}
}

// TestRedactDownloadURL verifies indexer API keys never survive into logs.
func TestRedactDownloadURL(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "strips apikey query",
			in:   "https://indexer.example/getnzb/abc?apikey=SECRET&id=1",
			want: "https://indexer.example/getnzb/abc",
		},
		{
			name: "strips fragment",
			in:   "https://indexer.example/download#frag",
			want: "https://indexer.example/download",
		},
		{
			name: "leaves clean url untouched",
			in:   "https://indexer.example/path/file.nzb",
			want: "https://indexer.example/path/file.nzb",
		},
		{
			name: "unparseable url is fully redacted",
			in:   "://not a url\x7f",
			want: "<redacted>",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactDownloadURL(tc.in)
			if got != tc.want {
				t.Fatalf("redactDownloadURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}

	if got := redactDownloadURL("https://x/y?apikey=TOPSECRET&r=TOPSECRET"); strings.Contains(got, "TOPSECRET") {
		t.Fatalf("secret leaked through redaction: %q", got)
	}
}
