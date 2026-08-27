package scanner

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

// TestSendManualSonarrSearch_SetsManualTrigger verifies AltMount asks Sonarr
// to bypass DelaySpecification by sending trigger=="manual" on repair
// searches. Without it, Sonarr evaluates the usenet delay profile against the
// just-deleted episode file and rejects every candidate release
// (altmount#847).
func TestSendManualSonarrSearch_SetsManualTrigger(t *testing.T) {
	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"EpisodeSearch","trigger":"manual"}`))
	}))
	defer server.Close()

	client := sonarr.New(&starr.Config{URL: server.URL, APIKey: "test"})

	resp, err := sendManualSonarrSearch(t.Context(), client, "EpisodeSearch", []int64{1703})
	if err != nil {
		t.Fatalf("sendManualSonarrSearch returned error: %v", err)
	}

	if trigger, _ := received["trigger"].(string); trigger != "manual" {
		t.Fatalf("expected request trigger %q, got %q (body: %+v)", "manual", trigger, received)
	}

	if name, _ := received["name"].(string); name != "EpisodeSearch" {
		t.Fatalf("expected request name %q, got %q", "EpisodeSearch", name)
	}

	if resp.Trigger != "manual" {
		t.Fatalf("expected response trigger %q, got %q", "manual", resp.Trigger)
	}
}

// TestSendManualRadarrSearch_SetsManualTrigger mirrors the Sonarr case for
// Radarr, keeping repair-search semantics consistent across the two targets
// (altmount#847).
func TestSendManualRadarrSearch_SetsManualTrigger(t *testing.T) {
	var received map[string]any

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&received); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"name":"MoviesSearch","trigger":"manual"}`))
	}))
	defer server.Close()

	client := radarr.New(&starr.Config{URL: server.URL, APIKey: "test"})

	resp, err := sendManualRadarrSearch(t.Context(), client, "MoviesSearch", []int64{42})
	if err != nil {
		t.Fatalf("sendManualRadarrSearch returned error: %v", err)
	}

	if trigger, _ := received["trigger"].(string); trigger != "manual" {
		t.Fatalf("expected request trigger %q, got %q (body: %+v)", "manual", trigger, received)
	}

	if resp.Trigger != "manual" {
		t.Fatalf("expected response trigger %q, got %q", "manual", resp.Trigger)
	}
}
