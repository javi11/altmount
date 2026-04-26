package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/javi11/altmount/internal/arrs/model"
	"golift.io/starr"
)

var (
	tvSeasonPattern = regexp.MustCompile(`(?i)S\d{1,4}E\d{1,4}`)
	tvDatePattern   = regexp.MustCompile(`(?i)\d{4}.\d{2}.\d{2}`)
)

// DiscoverFileMetadata attempts to find the rich metadata for a file by searching ARR instances.
// This implementation is 100% STRICT and performs zero fuzzy guessing.
func (m *Manager) DiscoverFileMetadata(ctx context.Context, filePath, relativePath, nzbName, libraryPath string) (*model.WebhookMetadata, error) {
	allInstances := m.instances.GetAllInstances()
	cleanNzbName := strings.TrimSuffix(nzbName, ".nzb")

	// Determine preferred type based on patterns (S01E01, etc.)
	isTV := tvSeasonPattern.MatchString(cleanNzbName) || tvSeasonPattern.MatchString(filePath) ||
		tvDatePattern.MatchString(cleanNzbName) || tvDatePattern.MatchString(filePath)

	preferredType := "radarr"
	if isTV {
		preferredType = "sonarr"
	}

	slog.DebugContext(ctx, "Strict Discovery: Starting", "nzb", cleanNzbName, "preferred_type", preferredType)

	// Strategy 1: Strict Global ID Lock (Primary Type First)
	if cleanNzbName != "" {
		// Pass 1: Preferred type
		for _, inst := range allInstances {
			if !inst.Enabled || inst.Type != preferredType {
				continue
			}

			if meta, err := m.runStrictDiscovery(ctx, inst, filePath, cleanNzbName, libraryPath); err == nil && meta != nil {
				slog.InfoContext(ctx, "Strict Discovery Success: Exact ID lock", "instance", inst.Name, "nzb", cleanNzbName, "type", inst.Type)
				return meta, nil
			}
		}

		// Pass 2: Fallback to other types (Only if preferred failed)
		for _, inst := range allInstances {
			if !inst.Enabled || inst.Type == preferredType {
				continue
			}

			if meta, err := m.runStrictDiscovery(ctx, inst, filePath, cleanNzbName, libraryPath); err == nil && meta != nil {
				slog.InfoContext(ctx, "Strict Discovery Success (Fallback Type): Exact ID lock", "instance", inst.Name, "nzb", cleanNzbName, "type", inst.Type)
				return meta, nil
			}
		}
	}

	return nil, fmt.Errorf("strict discovery failed: no exact match found for %s", filePath)
}

func (m *Manager) runStrictDiscovery(ctx context.Context, inst *model.ConfigInstance, filePath, cleanNzbName, libraryPath string) (*model.WebhookMetadata, error) {
	if inst.Type == "radarr" {
		return m.discoverRadarrStrict(ctx, filePath, cleanNzbName, libraryPath, inst.Name)
	} else if inst.Type == "sonarr" {
		return m.discoverSonarrStrict(ctx, filePath, cleanNzbName, libraryPath, inst.Name)
	}
	return nil, fmt.Errorf("unsupported type")
}

func (m *Manager) discoverRadarrStrict(ctx context.Context, filePath, cleanNzbName, libraryPath, instanceName string) (*model.WebhookMetadata, error) {
	instanceConfig, _ := m.instances.FindConfigInstance("radarr", instanceName)
	client, _ := m.clients.GetOrCreateRadarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)

	// 1. Check Library (Active Files)
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err == nil {
		for _, movie := range movies {
			if movie.HasFile && movie.MovieFile != nil {
				// Strict Match Conditions:
				// - Library Path matches
				// - OR Scene Name matches (NZB Name)
				if (libraryPath != "" && movie.MovieFile.Path == libraryPath) ||
					(cleanNzbName != "" && strings.EqualFold(movie.MovieFile.SceneName, cleanNzbName)) {
					metadata := &model.WebhookMetadata{
						EventType:    "StrictDiscovery",
						InstanceName: instanceName,
						Movie: &model.MovieMetadata{
							Id:     movie.ID,
							TmdbId: movie.TmdbID,
						},
						MovieFile: &model.MovieFileMetadata{
							Id:        movie.MovieFile.ID,
							SceneName: movie.MovieFile.SceneName,
						},
					}
					return metadata, nil
				}
			}
		}
	}

	// 2. Check History (Exact Release Name match)
	req := &starr.PageReq{PageSize: 100, SortKey: "date", SortDir: starr.SortDescend}
	history, err := client.GetHistoryPageContext(ctx, req)
	if err == nil {
		for _, record := range history.Records {
			if strings.EqualFold(record.SourceTitle, cleanNzbName) {
				metadata := &model.WebhookMetadata{
					EventType:    "StrictHistoryDiscovery",
					InstanceName: instanceName,
					Movie: &model.MovieMetadata{
						Id: record.MovieID,
					},
				}
				return metadata, nil
			}
		}
	}

	return nil, fmt.Errorf("not found")
}

func (m *Manager) discoverSonarrStrict(ctx context.Context, filePath, cleanNzbName, libraryPath, instanceName string) (*model.WebhookMetadata, error) {
	instanceConfig, _ := m.instances.FindConfigInstance("sonarr", instanceName)
	client, _ := m.clients.GetOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)

	// 1. Check Library (Active Files)
	series, err := m.data.GetSeries(ctx, client, instanceName)
	if err == nil {
		for _, show := range series {
			// Optimization: only pull files for shows that might contain this file
			if !strings.Contains(strings.ToLower(cleanNzbName), strings.ToLower(show.CleanTitle)) &&
				!strings.Contains(strings.ToLower(filePath), strings.ToLower(show.Path)) &&
				(libraryPath == "" || !strings.Contains(strings.ToLower(libraryPath), strings.ToLower(show.Path))) {
				continue
			}

			episodeFiles, err := m.data.GetEpisodeFiles(ctx, client, instanceName, show.ID)
			if err != nil {
				continue
			}
			for _, ef := range episodeFiles {
				if (libraryPath != "" && ef.Path == libraryPath) ||
					(cleanNzbName != "" && strings.EqualFold(ef.SceneName, cleanNzbName)) ||
					(ef.Path == filePath) {
					metadata := &model.WebhookMetadata{
						EventType:    "StrictDiscovery",
						InstanceName: instanceName,
						Series: &model.SeriesMetadata{
							Id:     show.ID,
							TvdbId: show.TvdbID,
						},
						EpisodeFile: &model.EpisodeFileMetadata{
							Id:        ef.ID,
							SceneName: ef.SceneName,
						},
					}
					return metadata, nil
				}
			}
		}
	}

	// 2. Check History (Exact Release Name match)
	req := &starr.PageReq{PageSize: 100, SortKey: "date", SortDir: starr.SortDescend}
	history, err := client.GetHistoryPageContext(ctx, req)
	if err == nil {
		for _, record := range history.Records {
			if strings.EqualFold(record.SourceTitle, cleanNzbName) {
				metadata := &model.WebhookMetadata{
					EventType:    "StrictHistoryDiscovery",
					InstanceName: instanceName,
					Series: &model.SeriesMetadata{
						Id: record.SeriesID,
					},
				}
				return metadata, nil
			}
		}
	}

	return nil, fmt.Errorf("not found")
}
