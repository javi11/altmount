package scanner

import (
	"context"
	"log/slog"

	"github.com/javi11/altmount/internal/arrs/failures"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

// splitEpisodesByBreaker bumps the failure breaker for every episode about to be
// re-searched and partitions them: episodes still under the configured threshold
// are searched, episodes at/over it are given up on (returned for unmonitoring).
// With the breaker disabled (threshold 0) all episodes are searched and nothing
// is counted.
func (m *Manager) splitEpisodesByBreaker(instanceName string, episodeIDs []int64) (search, giveUp []int64) {
	maxFailures := m.configGetter().Arrs.QueueCleanupMaxFailures
	if maxFailures <= 0 || m.failures == nil {
		return episodeIDs, nil
	}
	for _, id := range episodeIDs {
		if m.failures.Bump(failures.EpisodeKey(instanceName, id)) >= maxFailures {
			giveUp = append(giveUp, id)
		} else {
			search = append(search, id)
		}
	}
	return search, giveUp
}

// movieBreakerTripped bumps the failure breaker for a movie about to be
// re-searched and reports whether it has reached the give-up threshold.
func (m *Manager) movieBreakerTripped(instanceName string, movieID int64) bool {
	maxFailures := m.configGetter().Arrs.QueueCleanupMaxFailures
	if maxFailures <= 0 || m.failures == nil {
		return false
	}
	return m.failures.Bump(failures.MovieKey(instanceName, movieID)) >= maxFailures
}

// unmonitorSonarrEpisodes stops Sonarr/Whisparr from automatically re-searching
// the given episodes after the failure breaker tripped.
func unmonitorSonarrEpisodes(ctx context.Context, client *sonarr.Sonarr, instanceName string, episodeIDs []int64) {
	if len(episodeIDs) == 0 {
		return
	}
	if _, err := client.MonitorEpisodeContext(ctx, episodeIDs, false); err != nil {
		slog.WarnContext(ctx, "Failed to unmonitor episodes after failure threshold",
			"instance", instanceName, "episode_ids", episodeIDs, "error", err)
		return
	}
	slog.WarnContext(ctx, "Failure threshold reached, gave up on episodes (unmonitored, no re-search)",
		"instance", instanceName, "episode_ids", episodeIDs)
}

// unmonitorRadarrMovie stops Radarr from automatically re-searching the movie
// after the failure breaker tripped.
func unmonitorRadarrMovie(ctx context.Context, client *radarr.Radarr, instanceName string, movieID int64) {
	movie, err := client.GetMovieByIDContext(ctx, movieID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to unmonitor movie after failure threshold",
			"instance", instanceName, "movie_id", movieID, "error", err)
		return
	}
	if movie.Monitored {
		movie.Monitored = false
		if _, err := client.UpdateMovieContext(ctx, movieID, movie, false); err != nil {
			slog.WarnContext(ctx, "Failed to unmonitor movie after failure threshold",
				"instance", instanceName, "movie_id", movieID, "error", err)
			return
		}
	}
	slog.WarnContext(ctx, "Failure threshold reached, gave up on movie (unmonitored, no re-search)",
		"instance", instanceName, "movie_id", movieID)
}
