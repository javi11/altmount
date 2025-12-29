package data

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

const cacheTTL = 10 * time.Minute

type Manager struct {
	cacheMu      sync.RWMutex
	movieCache   map[string][]*radarr.Movie  // key: instance name
	seriesCache  map[string][]*sonarr.Series // key: instance name
	cacheExpiry  map[string]time.Time        // key: instance name
	requestGroup singleflight.Group
}

func NewManager() *Manager {
	return &Manager{
		movieCache:  make(map[string][]*radarr.Movie),
		seriesCache: make(map[string][]*sonarr.Series),
		cacheExpiry: make(map[string]time.Time),
	}
}

// GetMovies retrieves all movies from Radarr, using a cache if available and valid
func (m *Manager) GetMovies(ctx context.Context, client *radarr.Radarr, instanceName string) ([]*radarr.Movie, error) {
	// 1. Check cache (read lock)
	m.cacheMu.RLock()
	movies, ok := m.movieCache[instanceName]
	expiry, valid := m.cacheExpiry[instanceName]
	m.cacheMu.RUnlock()

	if ok && valid && time.Now().Before(expiry) {
		slog.DebugContext(ctx, "Using cached movie list", "instance", instanceName, "count", len(movies))
		return movies, nil
	}

	// 2. Use singleflight to deduplicate requests
	key := "radarr_movies_" + instanceName
	v, err, _ := m.requestGroup.Do(key, func() (interface{}, error) {
		// Double check cache
		m.cacheMu.RLock()
		movies, ok := m.movieCache[instanceName]
		expiry, valid := m.cacheExpiry[instanceName]
		m.cacheMu.RUnlock()
		if ok && valid && time.Now().Before(expiry) {
			return movies, nil
		}

		slog.DebugContext(ctx, "Fetching fresh movie list", "instance", instanceName)
		freshMovies, err := client.GetMovieContext(ctx, &radarr.GetMovie{})
		if err != nil {
			return nil, err
		}

		m.cacheMu.Lock()
		m.movieCache[instanceName] = freshMovies
		m.cacheExpiry[instanceName] = time.Now().Add(cacheTTL)
		m.cacheMu.Unlock()

		return freshMovies, nil
	})

	if err != nil {
		return nil, err
	}

	return v.([]*radarr.Movie), nil
}

// GetSeries retrieves all series from Sonarr, using a cache if available and valid
func (m *Manager) GetSeries(ctx context.Context, client *sonarr.Sonarr, instanceName string) ([]*sonarr.Series, error) {
	// 1. Check cache (read lock)
	m.cacheMu.RLock()
	series, ok := m.seriesCache[instanceName]
	expiry, valid := m.cacheExpiry[instanceName]
	m.cacheMu.RUnlock()

	if ok && valid && time.Now().Before(expiry) {
		slog.DebugContext(ctx, "Using cached series list", "instance", instanceName, "count", len(series))
		return series, nil
	}

	// 2. Use singleflight to deduplicate requests
	key := "sonarr_series_" + instanceName
	v, err, _ := m.requestGroup.Do(key, func() (interface{}, error) {
		// Double check cache
		m.cacheMu.RLock()
		series, ok := m.seriesCache[instanceName]
		expiry, valid := m.cacheExpiry[instanceName]
		m.cacheMu.RUnlock()
		if ok && valid && time.Now().Before(expiry) {
			return series, nil
		}

		slog.DebugContext(ctx, "Fetching fresh series list", "instance", instanceName)
		freshSeries, err := client.GetAllSeriesContext(ctx)
		if err != nil {
			return nil, err
		}

		m.cacheMu.Lock()
		m.seriesCache[instanceName] = freshSeries
		m.cacheExpiry[instanceName] = time.Now().Add(cacheTTL)
		m.cacheMu.Unlock()

		return freshSeries, nil
	})

	if err != nil {
		return nil, err
	}

	return v.([]*sonarr.Series), nil
}

