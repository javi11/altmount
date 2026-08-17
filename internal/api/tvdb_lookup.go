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

type cinemetaLookupResponse struct {
	Meta struct {
		Name      string `json:"name"`
		Year      string `json:"year"`
		MovieDBID int    `json:"moviedb_id"`
	} `json:"meta"`
}

// resolveSeriesMetadataFromIMDb resolves both TVDB ID and series title from an IMDb ID.
func resolveSeriesMetadataFromIMDb(ctx context.Context, imdbID string) (tvdbID, title string, err error) {
	if imdbID == "" {
		return "", "", nil
	}

	tvdbID, _ = resolveTVDBFromIMDb(ctx, imdbID)

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("https://v3-cinemeta.strem.io/meta/series/%s.json", url.PathEscape(imdbID)),
		nil,
	)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")

		if resp, err := tvmazeLookupClient.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var cData cinemetaLookupResponse
				if err := json.NewDecoder(resp.Body).Decode(&cData); err == nil {
					title = cData.Meta.Name
				}
			}
		}
	}

	return tvdbID, title, nil
}

// resolveMovieMetadataFromIMDb resolves TMDB ID, movie title, and release year from an IMDb ID.
func resolveMovieMetadataFromIMDb(ctx context.Context, imdbID string) (tmdbID int, title string, year string, err error) {
	if imdbID == "" {
		return 0, "", "", nil
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("https://v3-cinemeta.strem.io/meta/movie/%s.json", url.PathEscape(imdbID)),
		nil,
	)
	if err != nil {
		return 0, "", "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "altmount-stremio-movie-lookup")

	resp, err := tvmazeLookupClient.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var cData cinemetaLookupResponse
		if err := json.NewDecoder(resp.Body).Decode(&cData); err == nil {
			return cData.Meta.MovieDBID, cData.Meta.Name, cData.Meta.Year, nil
		}
	}

	return 0, "", "", nil
}
