package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Sportarr is a thin native HTTP client for Sportarr.
//
// Unlike Sonarr/Radarr/Lidarr/Readarr/Whisparr, Sportarr is NOT driveable through
// the golift.io/starr library: its /api/v3 surface is a narrow Sonarr-compat shim
// where only system/status and indexer are real, while /api/v3/queue and
// /api/v3/command fall through to the SPA. Sportarr's real, JSON-returning API
// lives at the unversioned /api/* paths (queue, history, pending-imports), so we
// talk to those directly here.
//
// The main management value AltMount adds for Sportarr is queue cleanup; Sportarr
// already self-imports finished downloads on its own poll interval.
type Sportarr struct {
	baseURL string
	apiKey  string
	hc      *http.Client
}

// SportarrStatusMessage mirrors the Servarr status-message shape used by the
// queue-cleanup allowlist matching.
type SportarrStatusMessage struct {
	Title    string   `json:"title"`
	Messages []string `json:"messages"`
}

// SportarrQueueItem maps the fields of a Sportarr native queue record that the
// queue-cleanup worker needs. Field names follow the Servarr convention; unknown
// fields are ignored by the JSON decoder.
type SportarrQueueItem struct {
	ID                    int64                   `json:"id"`
	Title                 string                  `json:"title"`
	Status                string                  `json:"status"`
	Protocol              string                  `json:"protocol"`
	DownloadClient        string                  `json:"downloadClient"`
	OutputPath            string                  `json:"outputPath"`
	TrackedDownloadStatus string                  `json:"trackedDownloadStatus"`
	TrackedDownloadState  string                  `json:"trackedDownloadState"`
	StatusMessages        []SportarrStatusMessage `json:"statusMessages"`
}

// NewSportarr builds a native Sportarr client. The shared *http.Client carries
// the manager's proxy/timeout configuration.
func NewSportarr(baseURL, apiKey string, hc *http.Client) *Sportarr {
	return &Sportarr{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		hc:      hc,
	}
}

func (s *Sportarr) newRequest(ctx context.Context, method, path string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", s.apiKey)
	req.Header.Set("Accept", "application/json")
	return req, nil
}

// Health verifies connectivity using the one /api/v3 endpoint Sportarr implements
// for real (system/status).
func (s *Sportarr) Health(ctx context.Context) error {
	req, err := s.newRequest(ctx, http.MethodGet, "/api/v3/system/status")
	if err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("failed to connect to Sportarr: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Sportarr health check returned status %d", resp.StatusCode)
	}
	return nil
}

// GetQueue returns the current Sportarr queue. Sportarr's native /api/queue may
// return either a bare JSON array or a paged object with a "records" array, so we
// tolerate both shapes.
func (s *Sportarr) GetQueue(ctx context.Context) ([]SportarrQueueItem, error) {
	req, err := s.newRequest(ctx, http.MethodGet, "/api/queue")
	if err != nil {
		return nil, err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to get Sportarr queue: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Sportarr queue request returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read Sportarr queue response: %w", err)
	}

	// Try the bare-array shape first.
	var items []SportarrQueueItem
	if err := json.Unmarshal(body, &items); err == nil {
		return items, nil
	}

	// Fall back to the paged-object shape.
	var paged struct {
		Records []SportarrQueueItem `json:"records"`
	}
	if err := json.Unmarshal(body, &paged); err != nil {
		return nil, fmt.Errorf("failed to decode Sportarr queue response: %w", err)
	}
	return paged.Records, nil
}

// DeleteQueueItem removes a queue item, also instructing Sportarr to remove the
// download from the (AltMount) download client. It does not blocklist.
func (s *Sportarr) DeleteQueueItem(ctx context.Context, id int64) error {
	path := fmt.Sprintf("/api/queue/%d?removeFromClient=true&blocklist=false", id)
	req, err := s.newRequest(ctx, http.MethodDelete, path)
	if err != nil {
		return err
	}
	resp, err := s.hc.Do(req)
	if err != nil {
		return fmt.Errorf("failed to delete Sportarr queue item %d: %w", id, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return fmt.Errorf("404: queue item %d not found", id)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("Sportarr delete queue item %d returned status %d", id, resp.StatusCode)
	}
	return nil
}
