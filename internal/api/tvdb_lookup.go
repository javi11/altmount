package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var tvmazeLookupClient = &http.Client{
	Timeout: 8 * time.Second,
}

type tvmazeLookupResponse struct {
	Externals struct {
		TheTVDB int `json:"thetvdb"`
	} `json:"externals"`
}

// resolveTVDBFromIMDb resolves a TVDB ID from an IMDb ID via the TVMaze lookup API.
// Returns an empty ID without error when the mapping does not exist.
func resolveTVDBFromIMDb(ctx context.Context, imdbID string) (string, error) {
	if imdbID == "" {
		return "", nil
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.tvmaze.com/lookup/shows?imdb="+url.QueryEscape(imdbID),
		nil,
	)
	if err != nil {
		return "", fmt.Errorf("create TVDB lookup request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")

	resp, err := tvmazeLookupClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("TVDB lookup request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return "", nil
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("TVDB lookup returned status %d: %s", resp.StatusCode, string(body))
	}

	var data tvmazeLookupResponse
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return "", fmt.Errorf("decode TVDB lookup response: %w", err)
	}
	if data.Externals.TheTVDB <= 0 {
		return "", nil
	}

	return strconv.Itoa(data.Externals.TheTVDB), nil
}
