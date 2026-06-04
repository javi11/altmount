package clients

import (
	"bytes"
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

// sportarrRef holds a Sportarr API reference value that, depending on the
// Sportarr build, serializes either as a bare string (older) or as an object
// like {"id":1,"name":"AltMount",...} (current). Only the name is needed.
// A custom unmarshaller keeps the queue decode from failing outright when the
// shape changes between versions.
type sportarrRef struct {
	Name string
}

func (r *sportarrRef) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		r.Name = ""
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, &r.Name)
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	r.Name = obj.Name
	return nil
}

// sportarrStatusNames maps the Sportarr DownloadStatus enum (serialized as an
// integer by current builds, which register no JsonStringEnumConverter) to its
// lowercased name. Order must match the enum declaration in Sportarr.
var sportarrStatusNames = []string{
	"queued", "downloading", "paused", "completed", "failed",
	"warning", "importing", "imported", "importpending", "importwarning",
}

// sportarrStatus accepts the download status whether the API serializes it as a
// string (older builds) or the numeric enum (current builds), normalizing to the
// lowercased status name so the cleanup comparisons keep working.
type sportarrStatus string

func (s *sportarrStatus) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*s = ""
		return nil
	}
	if b[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*s = sportarrStatus(strings.ToLower(str))
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	if n >= 0 && n < len(sportarrStatusNames) {
		*s = sportarrStatus(sportarrStatusNames[n])
	} else {
		*s = sportarrStatus(fmt.Sprintf("%d", n))
	}
	return nil
}

// sportarrStatusMessages accepts either the Servarr object shape
// [{title,messages}] (older builds) or a bare string array (current Sportarr),
// so the queue decode doesn't fail outright when the shape changes.
type sportarrStatusMessages []SportarrStatusMessage

func (m *sportarrStatusMessages) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) == 0 || string(b) == "null" {
		*m = nil
		return nil
	}
	// Object shape first.
	var objs []SportarrStatusMessage
	if err := json.Unmarshal(b, &objs); err == nil {
		*m = objs
		return nil
	}
	// Fall back to a bare string array.
	var strs []string
	if err := json.Unmarshal(b, &strs); err != nil {
		return err
	}
	out := make([]SportarrStatusMessage, 0, len(strs))
	for _, s := range strs {
		out = append(out, SportarrStatusMessage{Messages: []string{s}})
	}
	*m = out
	return nil
}

// SportarrQueueItem maps the fields of a Sportarr native queue record that the
// queue-cleanup worker needs. Field names follow the Servarr convention; unknown
// fields are ignored by the JSON decoder. status, statusMessages and
// downloadClient use tolerant types because their wire shapes differ between
// Sportarr builds and a mismatch would otherwise fail the whole queue decode.
type SportarrQueueItem struct {
	ID                    int64                  `json:"id"`
	Title                 string                 `json:"title"`
	Status                sportarrStatus         `json:"status"`
	Protocol              string                 `json:"protocol"`
	DownloadClient        sportarrRef            `json:"downloadClient"`
	DownloadID            string                 `json:"downloadId"`
	Indexer               string                 `json:"indexer"`
	OutputPath            string                 `json:"outputPath"`
	TrackedDownloadStatus string                 `json:"trackedDownloadStatus"`
	TrackedDownloadState  string                 `json:"trackedDownloadState"`
	StatusMessages        sportarrStatusMessages `json:"statusMessages"`
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
	return s.deleteQueueItem(ctx, id, false)
}

// DeleteQueueItemBlocklist removes a queue item and blocklists the release so the
// same NZB is not grabbed again, prompting Sportarr to search for a replacement.
func (s *Sportarr) DeleteQueueItemBlocklist(ctx context.Context, id int64) error {
	return s.deleteQueueItem(ctx, id, true)
}

func (s *Sportarr) deleteQueueItem(ctx context.Context, id int64, blocklist bool) error {
	path := fmt.Sprintf("/api/queue/%d?removeFromClient=true&blocklist=%t", id, blocklist)
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
