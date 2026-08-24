package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// maxMetadataBytes bounds TVmaze metadata responses (episode lists for
// long-running anime can reach a few MB).
const maxMetadataBytes = 8 << 20

var tvMetadataLookupClient = &http.Client{
	Timeout: 8 * time.Second,
}

type tvmazeLookupResponse struct {
	ID        int    `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Externals struct {
		TheTVDB int `json:"thetvdb"`
	} `json:"externals"`
}

// seriesTitleAliases returns alternative titles for the series (all
// languages, e.g. "Detective Conan" for romaji-primary anime). Empty on any
// failure — callers must treat nil as "no aliases known".
var seriesTitleAliasesCache sync.Map // imdbID -> []string

func resolveSeriesTitleAliases(ctx context.Context, imdbID string) []string {
	if imdbID == "" {
		return nil
	}
	if cached, ok := seriesTitleAliasesCache.Load(imdbID); ok {
		if aliases, _ := cached.([]string); len(aliases) > 0 {
			return aliases
		}
		return nil
	}
	showID, err := tvmazeShowIDForIMDb(ctx, imdbID)
	if err != nil || showID <= 0 {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.tvmaze.com/shows/"+strconv.Itoa(showID)+"/akas", nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")
	resp, err := tvMetadataLookupClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var akas []struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&akas); err != nil {
		return nil
	}
	seen := make(map[string]struct{}, len(akas))
	aliases := make([]string, 0, len(akas))
	for _, aka := range akas {
		name := strings.TrimSpace(aka.Name)
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		aliases = append(aliases, name)
	}
	if len(aliases) == 0 {
		return nil
	}
	seriesTitleAliasesCache.Store(imdbID, aliases)
	return aliases
}

// seriesEpisodeMeta maps (season, episode) to the franchise-absolute episode
// number and flags anime-style shows whose fansub releases use absolute
// numbering exclusively.
type seriesEpisodeMeta struct {
	absolute    map[int]map[int]int
	isAnimation bool
}

func (m seriesEpisodeMeta) absoluteFor(season, episode int) int {
	if m.absolute == nil {
		return 0
	}
	if eps, ok := m.absolute[season]; ok {
		return eps[episode]
	}
	return 0
}

var seriesEpisodeMetaCache sync.Map // imdbID -> seriesEpisodeMeta

func resolveSeriesEpisodeMeta(ctx context.Context, imdbID string) seriesEpisodeMeta {
	if imdbID == "" {
		return seriesEpisodeMeta{}
	}
	if cached, ok := seriesEpisodeMetaCache.Load(imdbID); ok {
		if meta, _ := cached.(seriesEpisodeMeta); meta.absolute != nil {
			return meta
		}
		return seriesEpisodeMeta{}
	}
	showID, metaType, err := tvmazeShowForIMDb(ctx, imdbID)
	if err != nil || showID <= 0 {
		return seriesEpisodeMeta{}
	}
	meta := seriesEpisodeMeta{isAnimation: strings.EqualFold(strings.TrimSpace(metaType), "Animation")}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.tvmaze.com/shows/"+strconv.Itoa(showID)+"/episodes", nil)
	if err != nil {
		return seriesEpisodeMeta{}
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")
	resp, err := tvMetadataLookupClient.Do(req)
	if err != nil {
		return seriesEpisodeMeta{}
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return seriesEpisodeMeta{}
	}
	var episodes []struct {
		SeasonNumber   int `json:"season"`
		EpisodeNumber  int `json:"number"`
		AbsoluteNumber int `json:"absolute_number"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&episodes); err != nil {
		return seriesEpisodeMeta{}
	}
	meta.absolute = make(map[int]map[int]int)
	for _, ep := range episodes {
		if ep.SeasonNumber <= 0 || ep.EpisodeNumber <= 0 {
			continue
		}
		seasonMap := meta.absolute[ep.SeasonNumber]
		if seasonMap == nil {
			seasonMap = make(map[int]int)
			meta.absolute[ep.SeasonNumber] = seasonMap
		}
		if ep.AbsoluteNumber > 0 {
			seasonMap[ep.EpisodeNumber] = ep.AbsoluteNumber
		} else if seasonMap[ep.EpisodeNumber] == 0 {
			seasonMap[ep.EpisodeNumber] = ep.EpisodeNumber
		}
	}
	seriesEpisodeMetaCache.Store(imdbID, meta)
	return meta
}

func tvmazeShowForIMDb(ctx context.Context, imdbID string) (showID int, showType string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.tvmaze.com/lookup/shows?imdb="+url.QueryEscape(imdbID), nil)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "altmount-stremio-tvdb-lookup")
	resp, err := tvMetadataLookupClient.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 0, "", fmt.Errorf("tvmaze lookup HTTP %d", resp.StatusCode)
	}
	var data tvmazeLookupResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataBytes)).Decode(&data); err != nil {
		return 0, "", err
	}
	return data.ID, data.Type, nil
}

func tvmazeShowIDForIMDb(ctx context.Context, imdbID string) (int, error) {
	id, _, err := tvmazeShowForIMDb(ctx, imdbID)
	return id, err
}

type cinemetaLookupResponse struct {
	Meta struct {
		Name   string `json:"name"`
		TVDBID int    `json:"tvdb_id"`
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
					if tvdbID == "" && cData.Meta.TVDBID > 0 {
						tvdbID = strconv.Itoa(cData.Meta.TVDBID)
					}
				}
			}
		}
	}

	return tvdbID, title, nil
}

type cinemetaMovieLookupResponse struct {
	Meta struct {
		Name      string `json:"name"`
		Year      string `json:"year"`
		MovieDBID int    `json:"moviedb_id"`
	} `json:"meta"`
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

	resp, err := tvMetadataLookupClient.Do(req)
	if err != nil {
		return 0, "", "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var cData cinemetaMovieLookupResponse
		if err := json.NewDecoder(resp.Body).Decode(&cData); err == nil {
			return cData.Meta.MovieDBID, cData.Meta.Name, cData.Meta.Year, nil
		}
	}

	return 0, "", "", nil
}
