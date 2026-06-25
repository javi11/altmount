package api

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/streamcheck"
)

// maxNzbCheckFetchSize caps how many bytes are read when fetching an NZB by URL
// for a stream check.
const maxNzbCheckFetchSize = 50 * 1024 * 1024 // 50 MB

// nzbCheckFetchTimeout bounds the NZB download portion of a single check.
const nzbCheckFetchTimeout = 30 * time.Second

// streamCheckItemConcurrency caps how many NZBs in a batch are checked at once.
// Per-item segment STATs are themselves bounded by the checker's max_connections,
// and the pool applies global connection admission on top of this.
const streamCheckItemConcurrency = 4

// StreamCheckItem is a single NZB to verify in a batch request.
type StreamCheckItem struct {
	// ID is an opaque caller-supplied identifier echoed back in the result so the
	// caller can correlate verdicts with its own candidate list.
	ID string `json:"id"`
	// NzbURL is the URL the NZB is downloaded from for verification.
	NzbURL string `json:"nzb_url"`
}

// StreamCheckRequest is the body of POST /api/nzb/check.
type StreamCheckRequest struct {
	// Items is the batch of NZBs to verify.
	Items []StreamCheckItem `json:"items"`
	// NzbURL is a convenience single-item form, used when Items is empty.
	NzbURL string `json:"nzb_url"`
}

// StreamCheckItemResult is the verdict for one requested NZB.
type StreamCheckItemResult struct {
	ID         string              `json:"id"`
	Verdict    streamcheck.Verdict `json:"verdict"`
	Checked    int                 `json:"checked"`
	Missing    int                 `json:"missing"`
	MissingPct float64             `json:"missing_pct"`
	Cached     bool                `json:"cached"`
	Error      string              `json:"error,omitempty"`
}

// StreamCheckResponse is the response of POST /api/nzb/check.
type StreamCheckResponse struct {
	Results []StreamCheckItemResult `json:"results"`
}

// handleNzbCheck handles POST /api/nzb/check.
//
// Public endpoint authenticated via the X-Api-Key header (raw API key) or a
// download_key query param (SHA256 of the API key). It verifies Usenet segment
// availability for one or more NZBs WITHOUT importing them, so clients such as
// AIOStreams can filter dead or incomplete releases before presenting them.
//
//	@Summary		Check NZB stream availability
//	@Description	Verifies that one or more NZBs' Usenet segments are still reachable (sampled NNTP STAT, no import). Auth: X-Api-Key header (raw API key) or download_key query param. Returns a verdict per item: available, degraded, dead, or unknown.
//	@Tags			Stremio
//	@Accept			json
//	@Produce		json
//	@Param			X-Api-Key	header		string				false	"Raw API key (alternative to download_key)"
//	@Param			body		body		StreamCheckRequest	true	"NZBs to check"
//	@Success		200			{object}	APIResponse{data=StreamCheckResponse}
//	@Failure		400			{object}	APIResponse
//	@Failure		401			{object}	APIResponse
//	@Failure		404			{object}	APIResponse
//	@Router			/nzb/check [post]
func (s *Server) handleNzbCheck(c *fiber.Ctx) error {
	ctx := c.Context()

	if s.configManager == nil {
		return RespondServiceUnavailable(c, "Configuration not available", "")
	}
	cfg := s.configManager.GetConfig()
	if cfg == nil || !cfg.GetStreamCheckEnabled() {
		return RespondNotFound(c, "Stream Check endpoint", "Stream Check is disabled in configuration")
	}

	// --- Authenticate via X-Api-Key header (raw key) or download_key (hashed) ---
	if rawKey := c.Get("X-Api-Key"); rawKey != "" {
		if !s.validateAPIKey(c, rawKey) {
			return RespondUnauthorized(c, "Invalid X-Api-Key", "")
		}
	} else {
		downloadKey := c.Query("download_key")
		if downloadKey == "" {
			return RespondUnauthorized(c, "Authentication required", "Provide X-Api-Key header or download_key")
		}
		if !s.validateDownloadKey(ctx, downloadKey) {
			return RespondUnauthorized(c, "Invalid download_key", "")
		}
	}

	if s.streamChecker == nil {
		return RespondServiceUnavailable(c, "Stream Check service not available", "")
	}

	var req StreamCheckRequest
	if err := c.BodyParser(&req); err != nil {
		return RespondBadRequest(c, "Invalid JSON in request body", err.Error())
	}

	items := req.Items
	if len(items) == 0 && req.NzbURL != "" {
		items = []StreamCheckItem{{ID: req.NzbURL, NzbURL: req.NzbURL}}
	}
	if len(items) == 0 {
		return RespondBadRequest(c, "No items to check", "Provide items[] or nzb_url")
	}

	if maxBatch := cfg.GetStreamCheckMaxBatch(); len(items) > maxBatch {
		items = items[:maxBatch]
	}

	results := make([]StreamCheckItemResult, len(items))
	sem := make(chan struct{}, streamCheckItemConcurrency)
	var wg sync.WaitGroup

	for i := range items {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item := items[i]
			out := StreamCheckItemResult{ID: item.ID, Verdict: streamcheck.VerdictUnknown}

			if item.NzbURL == "" {
				out.Error = "missing nzb_url"
				results[i] = out
				return
			}

			nzbData, err := fetchNzbForCheck(ctx, item.NzbURL)
			if err != nil {
				out.Error = err.Error()
				results[i] = out
				return
			}

			res, err := s.streamChecker.Check(ctx, nzbData)
			out.Verdict = res.Verdict
			out.Checked = res.Checked
			out.Missing = res.Missing
			out.MissingPct = res.MissingPct
			out.Cached = res.Cached
			if err != nil {
				out.Error = err.Error()
			}
			results[i] = out
		}(i)
	}
	wg.Wait()

	return RespondSuccess(c, StreamCheckResponse{Results: results})
}

// fetchNzbForCheck downloads an NZB by URL with a bounded size and timeout.
func fetchNzbForCheck(ctx context.Context, nzbURL string) ([]byte, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, nzbCheckFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(fetchCtx, http.MethodGet, nzbURL, nil) //nolint:gosec // URL is supplied by an authenticated caller
	if err != nil {
		return nil, fmt.Errorf("invalid nzb_url: %w", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch NZB: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch NZB: HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, maxNzbCheckFetchSize))
}
