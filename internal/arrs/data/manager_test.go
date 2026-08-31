package data

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

func TestManager_ClearCaches(t *testing.T) {
	m := NewManager()

	// Populate caches
	m.movieCache["radarr1"] = []*radarr.Movie{{ID: 1}}
	m.seriesCache["sonarr1"] = []*sonarr.Series{{ID: 2}}
	m.episodeFilesCache["sonarr_episode_files_sonarr1_10"] = []*sonarr.EpisodeFile{{ID: 3}}
	
	m.cacheExpiry["radarr1"] = time.Now().Add(10 * time.Minute)
	m.cacheExpiry["sonarr1"] = time.Now().Add(10 * time.Minute)
	m.cacheExpiry["sonarr_episode_files_sonarr1_10"] = time.Now().Add(10 * time.Minute)

	// Clear movies cache
	m.ClearMoviesCache("radarr1")
	assert.NotContains(t, m.movieCache, "radarr1")
	assert.NotContains(t, m.cacheExpiry, "radarr1")

	// Series and episode files should still be there
	assert.Contains(t, m.seriesCache, "sonarr1")
	assert.Contains(t, m.episodeFilesCache, "sonarr_episode_files_sonarr1_10")

	// Clear series cache
	m.ClearSeriesCache("sonarr1")
	assert.NotContains(t, m.seriesCache, "sonarr1")
	assert.NotContains(t, m.cacheExpiry, "sonarr1")
	assert.NotContains(t, m.episodeFilesCache, "sonarr_episode_files_sonarr1_10")
	assert.NotContains(t, m.cacheExpiry, "sonarr_episode_files_sonarr1_10")
}
