package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/data"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

type Manager struct {
	configGetter config.ConfigGetter
	instances    *instances.Manager
	clients      *clients.Manager
	data         *data.Manager
	sf           singleflight.Group
}

func NewManager(configGetter config.ConfigGetter, instances *instances.Manager, clients *clients.Manager, data *data.Manager) *Manager {
	return &Manager{
		configGetter: configGetter,
		instances:    instances,
		clients:      clients,
		data:         data,
	}
}

// findInstanceForFilePath finds which ARR instance manages the given file path
func (m *Manager) findInstanceForFilePath(ctx context.Context, filePath string, relativePath string) (instanceType string, instanceName string, err error) {
	slog.DebugContext(ctx, "Finding instance for file path", "file_path", filePath, "relative_path", relativePath)

	allInstances := m.instances.GetAllInstances()

	// Strategy 1: Fast Path - Check Root Folders
	for _, instance := range allInstances {
		if !instance.Enabled {
			continue
		}

		if client, err := m.clients.GetOrCreateClient(instance); err == nil {
			if m.managesFile(ctx, instance.Type, client, filePath) {
				return instance.Type, instance.Name, nil
			}
		}
	}

	// Strategy 2: Category Match - Check if file is in the staging/complete folder
	cfg := m.configGetter()
	if cfg.SABnzbd.CompleteDir != "" {
		// Normalize completeDir to a segment like "/complete/"
		completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")
		completeSegment := "/" + completeDir + "/"
		normalizedPath := filepath.ToSlash(filePath)

		// Check if path contains the complete directory as a segment
		if _, after, ok := strings.Cut(normalizedPath, completeSegment); ok {
			// Extract everything after the complete directory segment (e.g., "tv/show/file.mkv")
			afterPrefix := after
			parts := strings.Split(afterPrefix, "/")
			if len(parts) > 0 {
				category := parts[0]
				slog.DebugContext(ctx, "File is in complete_dir, matching by category", "category", category)

				for _, instance := range allInstances {
					if !instance.Enabled {
						continue
					}

					if strings.EqualFold(instance.Category, category) {
						slog.InfoContext(ctx, "Found managing instance by category in complete_dir", "instance", instance.Name, "category", category)
						return instance.Type, instance.Name, nil
					}
				}
			}
		}
	}

	// Strategy 3: Slow Path - Search Cache by Relative Path
	if relativePath != "" {
		slog.InfoContext(ctx, "Root folder match failed, attempting relative path search", "relative_path", relativePath)

		for _, instance := range allInstances {
			if !instance.Enabled {
				continue
			}

			if client, err := m.clients.GetOrCreateClient(instance); err == nil {
				if m.hasFile(ctx, instance.Type, client, instance.Name, relativePath) {
					slog.InfoContext(ctx, "Found managing instance by relative path", "instance", instance.Name, "type", instance.Type)
					return instance.Type, instance.Name, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("no ARR instance found managing file path: %s", filePath)
}

func (m *Manager) managesFile(ctx context.Context, instanceType string, client any, filePath string) bool {
	if instanceType == "radarr" {
		rc, ok := client.(*radarr.Radarr)
		if !ok {
			return false
		}
		return m.radarrManagesFile(ctx, rc, filePath)
	}
	sc, ok := client.(*sonarr.Sonarr)
	if !ok {
		return false
	}
	return m.sonarrManagesFile(ctx, sc, filePath)
}

func (m *Manager) hasFile(ctx context.Context, instanceType string, client any, instanceName, relativePath string) bool {
	if instanceType == "radarr" {
		rc, ok := client.(*radarr.Radarr)
		if !ok {
			return false
		}
		return m.radarrHasFile(ctx, rc, instanceName, relativePath)
	}
	sc, ok := client.(*sonarr.Sonarr)
	if !ok {
		return false
	}
	return m.sonarrHasFile(ctx, sc, instanceName, relativePath)
}

// radarrManagesFile checks if Radarr manages the given file path using root folders (checkrr approach)
func (m *Manager) radarrManagesFile(ctx context.Context, client *radarr.Radarr, filePath string) bool {
	slog.DebugContext(ctx, "Checking Radarr root folders for file ownership",
		"file_path", filePath)

	// Get root folders from Radarr (much faster than GetMovie)
	rootFolders, err := client.GetRootFoldersContext(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Failed to get root folders from Radarr for file check", "error", err)
		return false
	}

	// Check if file path starts with any root folder path
	for _, folder := range rootFolders {
		slog.DebugContext(ctx, "Checking Radarr root folder", "folder_path", folder.Path, "file_path", filePath)
		// Check for direct prefix match or if the filePath contains the folder.Path (common in Docker/Remote setups)
		if strings.HasPrefix(filePath, folder.Path) {
			slog.DebugContext(ctx, "File matches Radarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	slog.DebugContext(ctx, "File does not match any Radarr root folders")
	return false
}

// sonarrManagesFile checks if Sonarr manages the given file path using root folders (checkrr approach)
func (m *Manager) sonarrManagesFile(ctx context.Context, client *sonarr.Sonarr, filePath string) bool {
	slog.DebugContext(ctx, "Checking Sonarr root folders for file ownership",
		"file_path", filePath)

	// Get root folders from Sonarr (much faster than GetAllSeries)
	rootFolders, err := client.GetRootFoldersContext(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Failed to get root folders from Sonarr for file check", "error", err)
		return false
	}

	// Check if file path starts with any root folder path
	for _, folder := range rootFolders {
		slog.DebugContext(ctx, "Checking Sonarr root folder", "folder_path", folder.Path, "file_path", filePath)
		if strings.HasPrefix(filePath, folder.Path) {
			slog.DebugContext(ctx, "File matches Sonarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	slog.DebugContext(ctx, "File does not match any Sonarr root folders")
	return false
}

// radarrHasFile checks if any movie in the instance contains the given relative path
func (m *Manager) radarrHasFile(ctx context.Context, client *radarr.Radarr, instanceName, relativePath string) bool {
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get movies for relative path check", "instance", instanceName, "error", err)
		return false
	}

	strippedRelative := strings.TrimSuffix(relativePath, ".strm")

	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil {
			if strings.HasSuffix(movie.MovieFile.Path, relativePath) ||
				strings.HasSuffix(strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)), strippedRelative) {
				return true
			}
		}
	}
	return false
}

// sonarrHasFile checks if any series in the instance contains the given relative path
func (m *Manager) sonarrHasFile(ctx context.Context, client *sonarr.Sonarr, instanceName, relativePath string) bool {
	seriesList, err := m.data.GetSeries(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get series for relative path check", "instance", instanceName, "error", err)
		return false
	}

	// Normalize relative path for comparison
	relativePath = filepath.ToSlash(relativePath)
	strippedRelative := strings.TrimSuffix(relativePath, ".strm")

	for _, series := range seriesList {
		// Check if the series folder name is part of the relative path
		folderName := filepath.Base(series.Path)
		if strings.Contains(relativePath, folderName) || strings.Contains(strippedRelative, folderName) {
			return true
		}
	}
	return false
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
func (m *Manager) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string) error {
	res, err, _ := m.sf.Do(fmt.Sprintf("rescan:%s", pathForRescan), func() (interface{}, error) {
		slog.InfoContext(ctx, "Triggering ARR rescan", "path", pathForRescan, "relative_path", relativePath)

		// Find which ARR instance manages this file path
		instanceType, instanceName, err := m.findInstanceForFilePath(ctx, pathForRescan, relativePath)
		if err != nil {
			return nil, fmt.Errorf("failed to find ARR instance for file path %s: %w", pathForRescan, err)
		}

		// Find the instance configuration
		instanceConfig, err := m.instances.FindConfigInstance(instanceType, instanceName)
		if err != nil {
			return nil, fmt.Errorf("failed to find instance config: %w", err)
		}

		// Check if instance is enabled
		if !instanceConfig.Enabled {
			return nil, fmt.Errorf("instance %s/%s is disabled", instanceType, instanceName)
		}

		// Trigger rescan based on instance type
		switch instanceType {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to create Radarr client: %w", err)
			}
			return nil, m.triggerRadarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName)

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to create Sonarr client: %w", err)
			}
			return nil, m.triggerSonarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName)

		default:
			return nil, fmt.Errorf("unsupported instance type: %s", instanceType)
		}
	})

	if err != nil {
		return err
	}
	if res != nil {
		return res.(error)
	}
	return nil
}

// TriggerScanForFile finds the ARR instance managing the file and triggers a download scan on it.
func (m *Manager) TriggerScanForFile(ctx context.Context, filePath string) error {
	// Try to find which instance manages this file path
	instanceType, instanceName, err := m.findInstanceForFilePath(ctx, filePath, "")
	if err != nil {
		return err
	}

	instance, err := m.instances.FindConfigInstance(instanceType, instanceName)
	if err != nil {
		return err
	}

	if !instance.Enabled {
		return fmt.Errorf("instance %s is disabled", instanceName)
	}

	slog.InfoContext(ctx, "Triggering download scan for specific instance", "instance", instanceName, "type", instanceType)

	// Launch scan in background to not block caller
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		switch instance.Type {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to create Radarr client for scan trigger", "instance", instance.Name, "error", err)
				return
			}
			// Trigger RefreshMonitoredDownloads
			_, err = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", instance.Name, "error", err)
			} else {
				slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", instance.Name)
			}

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to create Sonarr client for scan trigger", "instance", instance.Name, "error", err)
				return
			}
			// Trigger RefreshMonitoredDownloads
			_, err = client.SendCommandContext(bgCtx, &sonarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", instance.Name, "error", err)
			} else {
				slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", instance.Name)
			}
		}
	}()

	return nil
}

// TriggerDownloadScan triggers the "Check For Finished Downloads" task in ARR instances
func (m *Manager) TriggerDownloadScan(ctx context.Context, instanceType string) {
	instances := m.instances.GetAllInstances()
	for _, instance := range instances {
		if !instance.Enabled || instance.Type != instanceType {
			continue
		}

		slog.DebugContext(ctx, "Triggering download client scan", "instance", instance.Name, "type", instance.Type)

		go func(inst *model.ConfigInstance) {
			_, _, _ = m.sf.Do(fmt.Sprintf("scan:%s", inst.Name), func() (interface{}, error) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				switch inst.Type {
				case "radarr":
					client, err := m.clients.GetOrCreateRadarrClient(inst.Name, inst.URL, inst.APIKey)
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to create Radarr client for scan trigger", "instance", inst.Name, "error", err)
						return nil, err
					}
					// Trigger RefreshMonitoredDownloads
					_, err = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", inst.Name, "error", err)
					} else {
						slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", inst.Name)
					}

				case "sonarr":
					client, err := m.clients.GetOrCreateSonarrClient(inst.Name, inst.URL, inst.APIKey)
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to create Sonarr client for scan trigger", "instance", inst.Name, "error", err)
						return nil, err
					}
					// Trigger RefreshMonitoredDownloads
					_, err = client.SendCommandContext(bgCtx, &sonarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", inst.Name, "error", err)
					} else {
						slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", inst.Name)
					}
				}
				return nil, nil
			})
		}(instance)
	}
}

// triggerRadarrRescanByPath triggers a rescan in Radarr for the given file path
func (m *Manager) triggerRadarrRescanByPath(ctx context.Context, client *radarr.Radarr, filePath, relativePath, instanceName string) error {
	slog.InfoContext(ctx, "Searching Radarr for matching movie",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath)

	// Get all movies to find the one with matching file path
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err != nil {
		return fmt.Errorf("failed to get movies from Radarr: %w", err)
	}

	var targetMovie *radarr.Movie
	for _, movie := range movies {
		// Try match by filename (the most robust way if paths differ)
		requestFileName := filepath.Base(filePath)

		if movie.HasFile && movie.MovieFile != nil {
			// Try exact match
			if movie.MovieFile.Path == filePath {
				targetMovie = movie
				break
			}

			movieFileName := filepath.Base(movie.MovieFile.Path)
			if movieFileName == requestFileName {
				slog.InfoContext(ctx, "Found Radarr movie match by filename",
					"movie", movie.Title,
					"path", movie.MovieFile.Path)
				targetMovie = movie
				break
			}

			// Try match without .strm extension if filePath is a .strm file
			if before, ok := strings.CutSuffix(filePath, ".strm"); ok {
				strippedPath := before
				// Check if movie file path (without its own extension) matches stripped filePath
				if strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)) == strippedPath {
					targetMovie = movie
					break
				}
			}
			// Try suffix match with relative path if provided
			if relativePath != "" {
				strippedRelative := strings.TrimSuffix(relativePath, ".strm")
				if strings.HasSuffix(movie.MovieFile.Path, relativePath) ||
					strings.HasSuffix(strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)), strippedRelative) {
					slog.InfoContext(ctx, "Found Radarr movie match by relative path suffix",
						"radarr_path", movie.MovieFile.Path,
						"relative_path", relativePath)
					targetMovie = movie
					break
				}
			}
		}
	}

	if targetMovie == nil {
		slog.WarnContext(ctx, "No movie found with matching file path in Radarr library, attempting queue-based failure",
			"instance", instanceName,
			"file_path", filePath)

		// Fallback: search in Radarr download queue for active/stuck imports
		if err := m.failRadarrQueueItemByPath(ctx, client, filePath); err == nil {
			return nil
		}

		return fmt.Errorf("no movie found with file path %s in library or queue: %w", filePath, model.ErrPathMatchFailed)
	}

	slog.InfoContext(ctx, "Found matching movie for file",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"movie_title", targetMovie.Title,
		"movie_path", targetMovie.Path,
		"file_path", filePath)

	// If we found the movie but it has no file (or different file), we can't blocklist the specific file ID
	// But we can still trigger search
	if targetMovie.HasFile && targetMovie.MovieFile != nil {
		// Try to blocklist the release associated with this file
		if err := m.blocklistRadarrMovieFile(ctx, client, targetMovie.ID, targetMovie.MovieFile.ID); err != nil {
			slog.WarnContext(ctx, "Failed to blocklist Radarr release", "error", err)
		}

		// Delete the existing file from Radarr database
		err = client.DeleteMovieFilesContext(ctx, targetMovie.MovieFile.ID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to delete movie file from Radarr, continuing with search",
				"instance", instanceName,
				"movie_id", targetMovie.ID,
				"file_id", targetMovie.MovieFile.ID,
				"error", err)
		}
	} else {
		slog.InfoContext(ctx, "Movie has no file linked in Radarr, skipping blocklist/delete and proceeding to search",
			"movie", targetMovie.Title)
	}

	// Step 3: Trigger targeted search for the missing movie
	searchCmd := &radarr.CommandRequest{
		Name:     "MoviesSearch",
		MovieIDs: []int64{targetMovie.ID},
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr search for movie ID %d: %w", targetMovie.ID, err)
	}

	slog.InfoContext(ctx, "Successfully triggered Radarr targeted search for re-download",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"command_id", response.ID)

	return nil
}

// triggerSonarrRescanByPath triggers a rescan in Sonarr for the given file path
func (m *Manager) triggerSonarrRescanByPath(ctx context.Context, client *sonarr.Sonarr, filePath, relativePath, instanceName string) error {
	cfg := m.configGetter()

	// Get library directory from health config
	libraryDir := m.configGetter().MountPath
	if cfg.Health.LibraryDir != nil && *cfg.Health.LibraryDir != "" {
		libraryDir = *cfg.Health.LibraryDir
	}

	slog.InfoContext(ctx, "Searching Sonarr for matching series",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath,
		"library_dir", libraryDir)

	// Get all series to find the one that contains this file path
	series, err := m.data.GetSeries(ctx, client, instanceName)
	if err != nil {
		return fmt.Errorf("failed to get series from Sonarr: %w", err)
	}

	// Find the series that contains this file path
	var targetSeries *sonarr.Series
	for _, show := range series {
		if strings.Contains(filePath, show.Path) {
			targetSeries = show
			break
		}
	}

	if targetSeries == nil {
		// Fallback search for series using relative path
		for _, show := range series {
			showFolderName := filepath.Base(show.Path)
			if strings.Contains(relativePath, showFolderName) {
				slog.InfoContext(ctx, "Found series match by folder name", "series", show.Title, "folder", showFolderName)
				targetSeries = show
				break
			}
		}
	}

	if targetSeries == nil {
		slog.WarnContext(ctx, "No series found in Sonarr matching file path in library, attempting queue-based failure",
			"instance", instanceName,
			"file_path", filePath)

		// Fallback: search in Sonarr download queue for active/stuck imports
		if err := m.failSonarrQueueItemByPath(ctx, client, filePath); err == nil {
			return nil
		}

		return fmt.Errorf("no series found containing file path in library or queue: %s", filePath)
	}

	slog.InfoContext(ctx, "Found matching series, searching for episode file",
		"series_title", targetSeries.Title,
		"series_path", targetSeries.Path)

	// Get all episodes for this specific series
	episodes, err := client.GetSeriesEpisodesContext(ctx, &sonarr.GetEpisode{
		SeriesID: targetSeries.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", targetSeries.Title, err)
	}

	// Get episode files for this series to find the matching file
	episodeFiles, err := m.data.GetEpisodeFiles(ctx, client, instanceName, targetSeries.ID)
	if err != nil {
		return fmt.Errorf("failed to get episode files for series %s: %w", targetSeries.Title, err)
	}

	// Find the episode file with matching path
	var targetEpisodeFile *sonarr.EpisodeFile
	for _, episodeFile := range episodeFiles {
		if episodeFile.Path == filePath {
			targetEpisodeFile = episodeFile
			break
		}

		// Try match by filename
		if filepath.Base(episodeFile.Path) == filepath.Base(filePath) {
			slog.InfoContext(ctx, "Found Sonarr episode match by filename", "path", episodeFile.Path)
			targetEpisodeFile = episodeFile
			break
		}

		// Try match without .strm extension
		if before, ok := strings.CutSuffix(filePath, ".strm"); ok {
			strippedPath := before
			if strings.TrimSuffix(episodeFile.Path, filepath.Ext(episodeFile.Path)) == strippedPath {
				targetEpisodeFile = episodeFile
				break
			}
		}

		// Try match with relative path
		if relativePath != "" {
			strippedRelative := strings.TrimSuffix(relativePath, ".strm")
			if strings.HasSuffix(episodeFile.Path, relativePath) ||
				strings.HasSuffix(strings.TrimSuffix(episodeFile.Path, filepath.Ext(episodeFile.Path)), strippedRelative) {
				slog.InfoContext(ctx, "Found Sonarr episode match by relative path suffix",
					"sonarr_path", episodeFile.Path,
					"relative_path", relativePath)
				targetEpisodeFile = episodeFile
				break
			}
		}
	}

	var episodeIDs []int64

	if targetEpisodeFile != nil {
		// Found the file record - get episodes linked to it
		for _, episode := range episodes {
			if episode.HasFile && episode.EpisodeFileID == targetEpisodeFile.ID {
				episodeIDs = append(episodeIDs, episode.ID)
			}
		}

		if len(episodeIDs) > 0 {
			slog.DebugContext(ctx, "Found matching episodes by file ID",
				"episode_count", len(episodeIDs),
				"episode_file_id", targetEpisodeFile.ID)

			// Try to blocklist the release associated with this file
			if err := m.blocklistSonarrEpisodeFile(ctx, client, targetSeries.ID, targetEpisodeFile.ID); err != nil {
				slog.WarnContext(ctx, "Failed to blocklist Sonarr release", "error", err)
			}

			// Delete the existing episode file from Sonarr database
			err = client.DeleteEpisodeFileContext(ctx, targetEpisodeFile.ID)
			if err != nil {
				slog.WarnContext(ctx, "Failed to delete episode file from Sonarr, continuing with search",
					"instance", instanceName,
					"episode_file_id", targetEpisodeFile.ID,
					"error", err)
			}
		}
	} else {
		slog.WarnContext(ctx, "Series found but no matching episode file found in Sonarr library, attempting queue-based failure",
			"series", targetSeries.Title,
			"file_path", filePath)

		// Fallback: search in Sonarr download queue
		if err := m.failSonarrQueueItemByPath(ctx, client, filePath); err == nil {
			return nil
		}
	}

	if len(episodeIDs) == 0 {
		return fmt.Errorf("no episodes found for file in library or queue: %s: %w", filePath, model.ErrPathMatchFailed)
	}

	// Trigger targeted episode search for all episodes in this file
	searchCmd := &sonarr.CommandRequest{
		Name:       "EpisodeSearch",
		EpisodeIDs: episodeIDs,
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Sonarr episode search: %w", err)
	}

	slog.InfoContext(ctx, "Successfully triggered Sonarr targeted episode search for re-download",
		"instance", instanceName,
		"series_title", targetSeries.Title,
		"episode_ids", episodeIDs,
		"command_id", response.ID)

	return nil
}

// failRadarrQueueItemByPath searches for an item in the active Radarr queue by path and marks it as failed
func (m *Manager) failRadarrQueueItemByPath(ctx context.Context, client *radarr.Radarr, path string) error {
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Radarr queue: %w", err)
	}

	for _, q := range queue.Records {
		// Try exact match, suffix match, or filename match
		if q.OutputPath == path ||
			(q.OutputPath != "" && strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(q.OutputPath))) ||
			(q.OutputPath != "" && filepath.Base(q.OutputPath) == filepath.Base(path)) {
			slog.InfoContext(ctx, "Found matching item in Radarr download queue, marking as failed",
				"queue_id", q.ID, "path", path, "output_path", q.OutputPath)

			removeFromClient := true
			opts := &starr.QueueDeleteOpts{
				RemoveFromClient: &removeFromClient,
				BlockList:        true,
				SkipRedownload:   false,
			}
			return client.DeleteQueueContext(ctx, q.ID, opts)
		}
	}

	return fmt.Errorf("no matching item found in Radarr queue for path: %s", path)
}

// failSonarrQueueItemByPath searches for an item in the active Sonarr queue by path and marks it as failed
func (m *Manager) failSonarrQueueItemByPath(ctx context.Context, client *sonarr.Sonarr, path string) error {
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr queue: %w", err)
	}

	for _, q := range queue.Records {
		// Try exact match, suffix match, or filename match
		if q.OutputPath == path ||
			(q.OutputPath != "" && strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(q.OutputPath))) ||
			(q.OutputPath != "" && filepath.Base(q.OutputPath) == filepath.Base(path)) {
			slog.InfoContext(ctx, "Found matching item in Sonarr download queue, marking as failed",
				"queue_id", q.ID, "path", path, "output_path", q.OutputPath)

			removeFromClient := true
			opts := &starr.QueueDeleteOpts{
				RemoveFromClient: &removeFromClient,
				BlockList:        true,
				SkipRedownload:   false,
			}
			return client.DeleteQueueContext(ctx, q.ID, opts)
		}
	}

	return fmt.Errorf("no matching item found in Sonarr queue for path: %s", path)
}

// blocklistRadarrMovieFile finds the history event for the given file and marks it as failed (blocklisting the release)
func (m *Manager) blocklistRadarrMovieFile(ctx context.Context, client *radarr.Radarr, movieID int64, fileID int64) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for movie file", "movie_id", movieID, "file_id", fileID)

	// Fetch history for this specific movie
	req := &starr.PageReq{PageSize: 100, SortKey: "date", SortDir: starr.SortDescend}
	req.Set("movieId", strconv.FormatInt(movieID, 10))

	history, err := client.GetHistoryPageContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to fetch Radarr history: %w", err)
	}

	targetFileID := strconv.FormatInt(fileID, 10)
	var downloadID string

	// 1. Find the import event to get the downloadId
	for _, record := range history.Records {
		if record.Data.FileID == targetFileID && (record.EventType == "movieFileImported" || record.EventType == "downloadFolderImported") {
			downloadID = record.DownloadID
			break
		}
	}

	if downloadID == "" {
		slog.WarnContext(ctx, "Could not find import event in Radarr history for file", "movie_id", movieID, "file_id", fileID)
		return nil
	}

	// 2. Find the original grab event using the downloadId
	for _, record := range history.Records {
		if record.DownloadID == downloadID && record.EventType == "grabbed" {
			slog.InfoContext(ctx, "Found grabbed history record, marking as failed to blocklist release",
				"history_id", record.ID, "download_id", downloadID)
			if failErr := client.FailContext(ctx, record.ID); failErr != nil {
				return fmt.Errorf("failed to fail Radarr grab event %d: %w", record.ID, failErr)
			}
			return nil
		}
	}

	slog.WarnContext(ctx, "Could not find grab event in Radarr history for download", "download_id", downloadID)
	return nil
}

// blocklistSonarrEpisodeFile finds the grabbed history event for the given file and marks it as failed (blocklisting the release)
func (m *Manager) blocklistSonarrEpisodeFile(ctx context.Context, client *sonarr.Sonarr, seriesID int64, fileID int64) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for episode file", "series_id", seriesID, "file_id", fileID)

	// Fetch history for this specific series
	req := &starr.PageReq{PageSize: 100, SortKey: "date", SortDir: starr.SortDescend}
	req.Set("seriesId", strconv.FormatInt(seriesID, 10))

	history, err := client.GetHistoryPageContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to fetch Sonarr history: %w", err)
	}

	targetFileID := strconv.FormatInt(fileID, 10)
	var downloadID string

	// 1. Find the import event to get the downloadId
	for _, record := range history.Records {
		if record.Data.FileID == targetFileID && record.EventType == "downloadFolderImported" {
			downloadID = record.DownloadID
			break
		}
	}

	if downloadID == "" {
		slog.WarnContext(ctx, "Could not find import event in Sonarr history for file", "series_id", seriesID, "file_id", fileID)
		return nil
	}

	// 2. Find the original grab event using the downloadId
	for _, record := range history.Records {
		if record.DownloadID == downloadID && record.EventType == "grabbed" {
			slog.InfoContext(ctx, "Found grabbed history record, marking as failed to blocklist release",
				"history_id", record.ID, "download_id", downloadID)
			if failErr := client.FailContext(ctx, record.ID); failErr != nil {
				return fmt.Errorf("failed to fail Sonarr grab event %d: %w", record.ID, failErr)
			}
			return nil
		}
	}

	slog.WarnContext(ctx, "Could not find grab event in Sonarr history for download", "download_id", downloadID)
	return nil
}
