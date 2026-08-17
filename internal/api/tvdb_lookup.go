package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

var tvMetadataLookupClient = &http.Client{
	Timeout: 8 * time.Second,
}

type tvmazeLookupResponse struct {
	Name      string `json:"name"`
	Externals struct {
		TheTVDB int `json:"thetvdb"`
	} `json:"externals"`
}

type cinemetaLookupResponse struct {
	Meta struct {
		Name string `json:"name"`
	} `json:"meta"`
}

// resolveSeriesMetadataFromIMDb resolves both TVDB ID and series title from an IMDb ID.
func resolveSeriesMetadataFromIMDb(ctx context.Context, imdbID string) (tvdbID, title string, err error) {
	if imdbID == "" {
		return "", "", nil
	}

	// 1. Try TVMaze lookup (gives both Title and TVDB ID)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		"https://api.tvmaze.com/lookup/shows?imdb="+url.QueryEscape(imdbID),
		nil,
	)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")

		if resp, err := tvMetadataLookupClient.Do(req); err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				var data tvmazeLookupResponse
				if err := json.NewDecoder(resp.Body).Decode(&data); err == nil {
					title = data.Name
					if data.Externals.TheTVDB > 0 {
						tvdbID = strconv.Itoa(data.Externals.TheTVDB)
					}
					if title != "" {
						return tvdbID, title, nil
					}
				}
			}
		}
	}

	// 2. Fallback to Cinemeta for series title if TVMaze missed title
	req, err = http.NewRequestWithContext(
		ctx,
		http.MethodGet,
		fmt.Sprintf("https://v3-cinemeta.strem.io/meta/series/%s.json", url.PathEscape(imdbID)),
		nil,
	)
	if err == nil {
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")

		if resp, err := tvMetadataLookupClient.Do(req); err == nil {
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

// resolveTVDBFromIMDb resolves a TVDB ID from an IMDb ID via the TVMaze lookup API.
// Returns an empty ID without error when the mapping does not exist.
func resolveTVDBFromIMDb(ctx context.Context, imdbID string) (string, error) {
	tvdbID, _, err := resolveSeriesMetadataFromIMDb(ctx, imdbID)
	return tvdbID, err
}
