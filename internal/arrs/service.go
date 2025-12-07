package arrs

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"golang.org/x/sync/singleflight"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

const cacheTTL = 10 * time.Minute

// ConfigInstance represents an arrs instance from configuration
type ConfigInstance struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // "radarr" or "sonarr"
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

// ConfigManager interface defines methods needed for configuration management
type ConfigManager interface {
	GetConfig() *config.Config
	UpdateConfig(config *config.Config) error
	SaveConfig() error
}

// Service manages Radarr and Sonarr instances for health monitoring and file repair
type Service struct {
	configGetter  config.ConfigGetter
	configManager ConfigManager
	mu            sync.RWMutex
	radarrClients map[string]*radarr.Radarr // key: instance name
	sonarrClients map[string]*sonarr.Sonarr // key: instance name

	// Caching
	cacheMu      sync.RWMutex
	movieCache   map[string][]*radarr.Movie  // key: instance name
	seriesCache  map[string][]*sonarr.Series // key: instance name
	cacheExpiry  map[string]time.Time        // key: instance name
	requestGroup singleflight.Group
}

// NewService creates a new arrs service for health monitoring and file repair
func NewService(configGetter config.ConfigGetter, configManager ConfigManager) *Service {
	return &Service{
		configGetter:  configGetter,
		configManager: configManager,
		radarrClients: make(map[string]*radarr.Radarr),
		sonarrClients: make(map[string]*sonarr.Sonarr),
		movieCache:    make(map[string][]*radarr.Movie),
		seriesCache:   make(map[string][]*sonarr.Series),
		cacheExpiry:   make(map[string]time.Time),
	}
}

// getConfigInstances returns all arrs instances from current configuration
func (s *Service) getConfigInstances() []*ConfigInstance {
	cfg := s.configGetter()
	instances := make([]*ConfigInstance, 0)

	// Convert Radarr instances
	if len(cfg.Arrs.RadarrInstances) > 0 {
		for _, radarrConfig := range cfg.Arrs.RadarrInstances {
			instance := &ConfigInstance{
				Name:    radarrConfig.Name,
				Type:    "radarr",
				URL:     radarrConfig.URL,
				APIKey:  radarrConfig.APIKey,
				Enabled: radarrConfig.Enabled != nil && *radarrConfig.Enabled,
			}
			instances = append(instances, instance)
		}
	}

	// Convert Sonarr instances
	if len(cfg.Arrs.SonarrInstances) > 0 {
		for _, sonarrConfig := range cfg.Arrs.SonarrInstances {
			instance := &ConfigInstance{
				Name:    sonarrConfig.Name,
				Type:    "sonarr",
				URL:     sonarrConfig.URL,
				APIKey:  sonarrConfig.APIKey,
				Enabled: sonarrConfig.Enabled != nil && *sonarrConfig.Enabled,
			}
			instances = append(instances, instance)
		}
	}

	return instances
}

// findConfigInstance finds a specific instance by type and name
func (s *Service) findConfigInstance(instanceType, instanceName string) (*ConfigInstance, error) {
	instances := s.getConfigInstances()
	for _, instance := range instances {
		if instance.Type == instanceType && instance.Name == instanceName {
			return instance, nil
		}
	}

	return nil, fmt.Errorf("instance not found: %s/%s", instanceType, instanceName)
}

// getOrCreateRadarrClient gets or creates a Radarr client for an instance
func (s *Service) getOrCreateRadarrClient(instanceName, url, apiKey string) (*radarr.Radarr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.radarrClients[instanceName]; exists {
		return client, nil
	}

	client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
	s.radarrClients[instanceName] = client
	return client, nil
}

// getOrCreateSonarrClient gets or creates a Sonarr client for an instance
func (s *Service) getOrCreateSonarrClient(instanceName, url, apiKey string) (*sonarr.Sonarr, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if client, exists := s.sonarrClients[instanceName]; exists {
		return client, nil
	}

	client := sonarr.New(&starr.Config{URL: url, APIKey: apiKey})
	s.sonarrClients[instanceName] = client
	return client, nil
}

// findInstanceForFilePath finds which ARR instance manages the given file path
func (s *Service) findInstanceForFilePath(ctx context.Context, filePath string, relativePath string) (instanceType string, instanceName string, err error) {
	slog.DebugContext(ctx, "Finding instance for file path", "file_path", filePath, "relative_path", relativePath)

	// Strategy 1: Fast Path - Check Root Folders
	// This is the preferred method as it's O(1) per instance with root folders loaded
	for _, instance := range s.getConfigInstances() {
		if !instance.Enabled {
			continue
		}

		slog.DebugContext(ctx, "Checking instance for file (Root Folder Strategy)",
			"instance_name", instance.Name,
			"instance_type", instance.Type,
			"file_path", filePath)

		switch instance.Type {
		case "radarr":
			client, err := s.getOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				continue
			}
			if s.radarrManagesFile(ctx, client, filePath) {
				return "radarr", instance.Name, nil
			}

		case "sonarr":
			client, err := s.getOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				continue
			}
			if s.sonarrManagesFile(ctx, client, filePath) {
				return "sonarr", instance.Name, nil
			}
		}
	}

	// Strategy 2: Slow Path - Search Cache by Relative Path
	// If relativePath is provided, we can search our cached movies/series to see if any contain this file
	if relativePath != "" {
		slog.InfoContext(ctx, "Root folder match failed, attempting relative path search", "relative_path", relativePath)

		for _, instance := range s.getConfigInstances() {
			if !instance.Enabled {
				continue
			}

			switch instance.Type {
			case "radarr":
				client, err := s.getOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
				if err != nil {
					continue
				}
				// Use helper that utilizes cache
				if s.radarrHasFile(ctx, client, instance.Name, relativePath) {
					slog.InfoContext(ctx, "Found managing Radarr instance by relative path", "instance", instance.Name)
					return "radarr", instance.Name, nil
				}

			case "sonarr":
				client, err := s.getOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
				if err != nil {
					continue
				}
				// Use helper that utilizes cache
				if s.sonarrHasFile(ctx, client, instance.Name, relativePath) {
					slog.InfoContext(ctx, "Found managing Sonarr instance by relative path", "instance", instance.Name)
					return "sonarr", instance.Name, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("no ARR instance found managing file path: %s", filePath)
}

// radarrHasFile checks if any movie in the instance contains the given relative path
func (s *Service) radarrHasFile(ctx context.Context, client *radarr.Radarr, instanceName, relativePath string) bool {
	movies, err := s.getMovies(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get movies for relative path check", "instance", instanceName, "error", err)
		return false
	}

	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil {
			if strings.HasSuffix(movie.MovieFile.Path, relativePath) {
				return true
			}
		}
	}
	return false
}

// sonarrHasFile checks if any series in the instance contains the given relative path
// Note: This is an approximation. We check if the series path + relative path structure makes sense,
// or if we can find the specific episode file. But 'series' objects don't contain episode files directly.
// We'd need to fetch episode files for ALL series to be 100% sure, which is too expensive even with caching.
// So we check if the series PATH contains part of the relative path, or if the relative path looks like it belongs to the series.
func (s *Service) sonarrHasFile(ctx context.Context, client *sonarr.Sonarr, instanceName, relativePath string) bool {
	seriesList, err := s.getSeries(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get series for relative path check", "instance", instanceName, "error", err)
		return false
	}

	// Normalize relative path for comparison
	relativePath = filepath.ToSlash(relativePath)

	for _, series := range seriesList {
		// Check if the series folder name is part of the relative path
		// e.g. RelativePath: "TV/Show Name/Season 1/Ep.mkv"
		// Series Path: "/mnt/media/TV/Show Name"
		// Folder Name: "Show Name"
		folderName := filepath.Base(series.Path)
		if strings.Contains(relativePath, folderName) {
			return true
		}
	}
	return false
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
// The pathForRescan should be the library path (symlink or .strm file) if available,
// otherwise the mount path. It's the caller's responsibility to find the appropriate path.
func (s *Service) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string) error {
	slog.InfoContext(ctx, "Triggering ARR rescan", "path", pathForRescan, "relative_path", relativePath)

	// Find which ARR instance manages this file path
	instanceType, instanceName, err := s.findInstanceForFilePath(ctx, pathForRescan, relativePath)
	if err != nil {
		return fmt.Errorf("failed to find ARR instance for file path %s: %w", pathForRescan, err)
	}

	// Find the instance configuration
	instanceConfig, err := s.findConfigInstance(instanceType, instanceName)
	if err != nil {
		return fmt.Errorf("failed to find instance config: %w", err)
	}

	// Check if instance is enabled
	if !instanceConfig.Enabled {
		return fmt.Errorf("instance %s/%s is disabled", instanceType, instanceName)
	}

	// Trigger rescan based on instance type
	switch instanceType {
	case "radarr":
		client, err := s.getOrCreateRadarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
		if err != nil {
			return fmt.Errorf("failed to create Radarr client: %w", err)
		}
		return s.triggerRadarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName)

	case "sonarr":
		client, err := s.getOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
		if err != nil {
			return fmt.Errorf("failed to create Sonarr client: %w", err)
		}
		return s.triggerSonarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName)

	default:
		return fmt.Errorf("unsupported instance type: %s", instanceType)
	}
}

// getMovies retrieves all movies from Radarr, using a cache if available and valid
func (s *Service) getMovies(ctx context.Context, client *radarr.Radarr, instanceName string) ([]*radarr.Movie, error) {
	// 1. Check cache (read lock)
	s.cacheMu.RLock()
	movies, ok := s.movieCache[instanceName]
	expiry, valid := s.cacheExpiry[instanceName]
	s.cacheMu.RUnlock()

	if ok && valid && time.Now().Before(expiry) {
		slog.DebugContext(ctx, "Using cached movie list", "instance", instanceName, "count", len(movies))
		return movies, nil
	}

	// 2. Use singleflight to deduplicate requests
	key := "radarr_movies_" + instanceName
	v, err, _ := s.requestGroup.Do(key, func() (interface{}, error) {
		// Double check cache
		s.cacheMu.RLock()
		movies, ok := s.movieCache[instanceName]
		expiry, valid := s.cacheExpiry[instanceName]
		s.cacheMu.RUnlock()
		if ok && valid && time.Now().Before(expiry) {
			return movies, nil
		}

		slog.DebugContext(ctx, "Fetching fresh movie list", "instance", instanceName)
		freshMovies, err := client.GetMovieContext(ctx, &radarr.GetMovie{})
		if err != nil {
			return nil, err
		}

		s.cacheMu.Lock()
		s.movieCache[instanceName] = freshMovies
		s.cacheExpiry[instanceName] = time.Now().Add(cacheTTL)
		s.cacheMu.Unlock()

		return freshMovies, nil
	})

	if err != nil {
		return nil, err
	}

	return v.([]*radarr.Movie), nil
}

// getSeries retrieves all series from Sonarr, using a cache if available and valid
func (s *Service) getSeries(ctx context.Context, client *sonarr.Sonarr, instanceName string) ([]*sonarr.Series, error) {
	// 1. Check cache (read lock)
	s.cacheMu.RLock()
	series, ok := s.seriesCache[instanceName]
	expiry, valid := s.cacheExpiry[instanceName]
	s.cacheMu.RUnlock()

	if ok && valid && time.Now().Before(expiry) {
		slog.DebugContext(ctx, "Using cached series list", "instance", instanceName, "count", len(series))
		return series, nil
	}

	// 2. Use singleflight to deduplicate requests
	key := "sonarr_series_" + instanceName
	v, err, _ := s.requestGroup.Do(key, func() (interface{}, error) {
		// Double check cache
		s.cacheMu.RLock()
		series, ok := s.seriesCache[instanceName]
		expiry, valid := s.cacheExpiry[instanceName]
		s.cacheMu.RUnlock()
		if ok && valid && time.Now().Before(expiry) {
			return series, nil
		}

		slog.DebugContext(ctx, "Fetching fresh series list", "instance", instanceName)
		freshSeries, err := client.GetAllSeriesContext(ctx)
		if err != nil {
			return nil, err
		}

		s.cacheMu.Lock()
		s.seriesCache[instanceName] = freshSeries
		s.cacheExpiry[instanceName] = time.Now().Add(cacheTTL)
		s.cacheMu.Unlock()

		return freshSeries, nil
	})

	if err != nil {
		return nil, err
	}

	return v.([]*sonarr.Series), nil
}

// radarrManagesFile checks if Radarr manages the given file path using root folders (checkrr approach)
func (s *Service) radarrManagesFile(ctx context.Context, client *radarr.Radarr, filePath string) bool {
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
		if strings.HasPrefix(filePath, folder.Path) {
			slog.DebugContext(ctx, "File matches Radarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	slog.DebugContext(ctx, "File does not match any Radarr root folders")
	return false
}

// triggerRadarrRescanByPath triggers a rescan in Radarr for the given file path
func (s *Service) triggerRadarrRescanByPath(ctx context.Context, client *radarr.Radarr, filePath, relativePath, instanceName string) error {
	slog.DebugContext(ctx, "Checking Radarr for file path",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath)

	// Get all movies to find the one with matching file path
	movies, err := s.getMovies(ctx, client, instanceName)
	if err != nil {
		return fmt.Errorf("failed to get movies from Radarr: %w", err)
	}

	var targetMovie *radarr.Movie
	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil {
			// Try exact match
			if movie.MovieFile.Path == filePath {
				targetMovie = movie
				break
			}
			// Try suffix match with relative path if provided
			if relativePath != "" && strings.HasSuffix(movie.MovieFile.Path, relativePath) {
				slog.InfoContext(ctx, "Found Radarr movie match by relative path suffix",
					"radarr_path", movie.MovieFile.Path,
					"relative_path", relativePath)
				targetMovie = movie
				break
			}
		}
	}

	if targetMovie == nil {
		slog.DebugContext(ctx, "No movie found with file path",
			"instance", instanceName,
			"file_path", filePath)

		return fmt.Errorf("no movie found with file path: %s. Check if the movie has any files", filePath)
	}

	slog.DebugContext(ctx, "Found matching movie for file",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"movie_title", targetMovie.Title,
		"movie_path", targetMovie.Path,
		"file_path", filePath)

	// Try to blocklist the release associated with this file
	if err := s.blocklistRadarrMovieFile(ctx, client, targetMovie.ID, targetMovie.MovieFile.ID); err != nil {
		slog.WarnContext(ctx, "Failed to blocklist Radarr release", "error", err)
	}

	// Delete the existing file
	err = client.DeleteMovieFilesContext(ctx, targetMovie.MovieFile.ID)
	if err != nil {
		slog.WarnContext(ctx, "Failed to delete movie file, continuing with rescan",
			"instance", instanceName,
			"movie_id", targetMovie.ID,
			"file_id", targetMovie.MovieFile.ID,
			"error", err)
	}

	// Trigger rescan for the movie
	_, err = client.SendCommandContext(ctx, &radarr.CommandRequest{
		Name:     "RescanMovie",
		MovieIDs: []int64{targetMovie.ID},
	})
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr rescan for movie ID %d: %w", targetMovie.ID, err)
	}

	slog.DebugContext(ctx, "Successfully triggered Radarr rescan",
		"instance", instanceName,
		"movie_id", targetMovie.ID)

	// Step 3: Trigger search for the missing movie
	// We've deleted the file, so now we need to search for a replacement
	searchCmd := &radarr.CommandRequest{
		Name:     "MoviesSearch",
		MovieIDs: []int64{targetMovie.ID},
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr search for movie ID %d: %w", targetMovie.ID, err)
	}

	slog.InfoContext(ctx, "Successfully triggered Radarr search for re-download",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"command_id", response.ID)

	return nil
}

// sonarrManagesFile checks if Sonarr manages the given file path using root folders (checkrr approach)
func (s *Service) sonarrManagesFile(ctx context.Context, client *sonarr.Sonarr, filePath string) bool {
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

// triggerSonarrRescanByPath triggers a rescan in Sonarr for the given file path
func (s *Service) triggerSonarrRescanByPath(ctx context.Context, client *sonarr.Sonarr, filePath, relativePath, instanceName string) error {
	cfg := s.configGetter()

	// Get library directory from health config
	libraryDir := ""
	if cfg.Health.LibraryDir != nil && *cfg.Health.LibraryDir != "" {
		libraryDir = *cfg.Health.LibraryDir
	} else {
		return fmt.Errorf("Health.LibraryDir is not configured")
	}

	slog.DebugContext(ctx, "Triggering Sonarr rescan/re-download by path",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath,
		"library_dir", libraryDir)

	// Get all series to find the one that contains this file path
	series, err := s.getSeries(ctx, client, instanceName)
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
		// Fallback: check if series path is contained in relative path (less likely but possible if mounts differ)
		// Or checking if show path contains relative path parts?
		// Better logic: If we have relativePath "tv/Show/Season/Ep.mkv", we can try to match Series Title or Folder.
		// But 'findInstanceForFilePath' already matched the root folder.
		// If we are here, we assume 'series' search below might fail if 'filePath' prefix is wrong.
		// But let's stick to 'filePath' for Series lookup for now, as 'findInstanceForFilePath' relies on it.
		// If 'findInstance' worked, then 'filePath' matches a RootFolder prefix.
		// So 'show.Path' (which starts with RootFolder) should match 'filePath'.
	}

	if targetSeries == nil {
		// Fallback search for series using relative path
		// This is risky if we have multiple root folders, but worth a try if exact match fails
		for _, show := range series {
			// Check if the show's path component (e.g. "Show Name") is in the relative path
			// relativePath might be "tv/Show Name/..."
			// show.Path might be "/mnt/media/tv/Show Name"
			showFolderName := filepath.Base(show.Path)
			if strings.Contains(relativePath, showFolderName) {
				slog.DebugContext(ctx, "Found potential series match by folder name", "series", show.Title, "folder", showFolderName)
				targetSeries = show
				break
			}
		}
	}

	if targetSeries == nil {
		return fmt.Errorf("no series found containing file path: %s", filePath)
	}

	slog.DebugContext(ctx, "Found matching series for file",
		"series_title", targetSeries.Title,
		"series_path", targetSeries.Path,
		"file_path", filePath)

	// Get all episodes for this specific series
	episodes, err := client.GetSeriesEpisodesContext(ctx, &sonarr.GetEpisode{
		SeriesID: targetSeries.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", targetSeries.Title, err)
	}

	// Get episode files for this series to find the matching file
	episodeFiles, err := client.GetSeriesEpisodeFilesContext(ctx, targetSeries.ID)
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
		// Try match with relative path
		if relativePath != "" && strings.HasSuffix(episodeFile.Path, relativePath) {
			slog.InfoContext(ctx, "Found Sonarr episode match by relative path suffix",
				"sonarr_path", episodeFile.Path,
				"relative_path", relativePath)
			targetEpisodeFile = episodeFile
			break
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
			if err := s.blocklistSonarrEpisodeFile(ctx, client, targetSeries.ID, targetEpisodeFile.ID); err != nil {
				slog.WarnContext(ctx, "Failed to blocklist Sonarr release", "error", err)
			}

			// Delete the existing episode file
			err = client.DeleteEpisodeFileContext(ctx, targetEpisodeFile.ID)
			if err != nil {
				slog.WarnContext(ctx, "Failed to delete episode file",
					"instance", instanceName,
					"episode_file_id", targetEpisodeFile.ID,
					"error", err)
			}
		}
	}

	if len(episodeIDs) == 0 {
		return fmt.Errorf("no episodes found for file: %s (path match failed)", filePath)
	}

	// Trigger episode search for all episodes in this file
	searchCmd := &sonarr.CommandRequest{
		Name:       "EpisodeSearch",
		EpisodeIDs: episodeIDs,
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger episode search: %w", err)
	}

	slog.DebugContext(ctx, "Successfully triggered episode search for re-download",
		"instance", instanceName,
		"series_title", targetSeries.Title,
		"episode_ids", episodeIDs,
		"command_id", response.ID)

	return nil
}

// TriggerDownloadScan triggers the "Check For Finished Downloads" task in ARR instances
func (s *Service) TriggerDownloadScan(ctx context.Context, instanceType string) {
	instances := s.getConfigInstances()
	for _, instance := range instances {
		if !instance.Enabled || instance.Type != instanceType {
			continue
		}

		slog.DebugContext(ctx, "Triggering download client scan", "instance", instance.Name, "type", instance.Type)

		go func(inst *ConfigInstance) {
			// Use a new background context for the async call
			bgCtx := context.Background()

			switch inst.Type {
			case "radarr":
				client, err := s.getOrCreateRadarrClient(inst.Name, inst.URL, inst.APIKey)
				if err != nil {
					slog.ErrorContext(bgCtx, "Failed to create Radarr client for scan trigger", "instance", inst.Name, "error", err)
					return
				}
				// Trigger DownloadedMoviesScan
				_, err = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "DownloadedMoviesScan"})
				if err != nil {
					slog.ErrorContext(bgCtx, "Failed to trigger DownloadedMoviesScan", "instance", inst.Name, "error", err)
				} else {
					slog.InfoContext(bgCtx, "Triggered DownloadedMoviesScan", "instance", inst.Name)
				}

			case "sonarr":
				client, err := s.getOrCreateSonarrClient(inst.Name, inst.URL, inst.APIKey)
				if err != nil {
					slog.ErrorContext(bgCtx, "Failed to create Sonarr client for scan trigger", "instance", inst.Name, "error", err)
					return
				}
				// Trigger DownloadedEpisodesScan
				_, err = client.SendCommandContext(bgCtx, &sonarr.CommandRequest{Name: "DownloadedEpisodesScan"})
				if err != nil {
					slog.ErrorContext(bgCtx, "Failed to trigger DownloadedEpisodesScan", "instance", inst.Name, "error", err)
				} else {
					slog.InfoContext(bgCtx, "Triggered DownloadedEpisodesScan", "instance", inst.Name)
				}
			}
		}(instance)
	}
}

// GetAllInstances returns all arrs instances from configuration
func (s *Service) GetAllInstances() []*ConfigInstance {
	return s.getConfigInstances()
}

// generateInstanceName generates an instance name from a URL
// Format: hostname-port (e.g., "localhost-8989", "sonarr.local-80")
func (s *Service) generateInstanceName(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Get hostname and port
	hostname := parsedURL.Hostname()
	port := parsedURL.Port()

	// If no port specified, use default based on scheme
	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	// Combine hostname and port
	return fmt.Sprintf("%s-%s", hostname, port), nil
}

// normalizeURL normalizes a URL for comparison by removing trailing slashes
func normalizeURL(rawURL string) string {
	return strings.TrimSuffix(rawURL, "/")
}

// instanceExistsByURL checks if an instance with the given URL already exists
func (s *Service) instanceExistsByURL(checkURL string) bool {
	normalizedCheck := normalizeURL(checkURL)
	instances := s.getConfigInstances()

	for _, instance := range instances {
		normalizedInstance := normalizeURL(instance.URL)
		if normalizedInstance == normalizedCheck {
			return true
		}
	}

	return false
}

// detectARRType attempts to detect if a URL points to Radarr or Sonarr
// Returns "radarr", "sonarr", or an error if neither can be determined
func (s *Service) detectARRType(ctx context.Context, arrURL, apiKey string) (string, error) {
	slog.DebugContext(ctx, "Detecting ARR type", "url", arrURL)

	// Try Radarr first
	radarrClient := radarr.New(&starr.Config{URL: arrURL, APIKey: apiKey})
	radarrStatus, err := radarrClient.GetSystemStatusContext(ctx)
	if err == nil {
		// Check AppName from the result
		switch radarrStatus.AppName {
		case "Radarr":
			slog.DebugContext(ctx, "Detected Radarr instance", "url", arrURL)
			return "radarr", nil
		case "Sonarr":
			slog.DebugContext(ctx, "Detected Sonarr instance", "url", arrURL)
			return "sonarr", nil
		default:
			slog.DebugContext(ctx, "Unknown AppName from Radarr client", "app_name", radarrStatus.AppName, "url", arrURL)
		}
	}

	// Try Sonarr
	sonarrClient := sonarr.New(&starr.Config{URL: arrURL, APIKey: apiKey})
	sonarrStatus, err := sonarrClient.GetSystemStatusContext(ctx)
	if err == nil {
		// Check AppName from the result
		switch sonarrStatus.AppName {
		case "Radarr":
			slog.DebugContext(ctx, "Detected Radarr instance", "url", arrURL)
			return "radarr", nil
		case "Sonarr":
			slog.DebugContext(ctx, "Detected Sonarr instance", "url", arrURL)
			return "sonarr", nil
		default:
			slog.DebugContext(ctx, "Unknown AppName from Sonarr client", "app_name", sonarrStatus.AppName, "url", arrURL)
		}
	}

	return "", fmt.Errorf("unable to detect ARR type for URL %s - neither Radarr nor Sonarr responded successfully", arrURL)
}

// RegisterInstance attempts to automatically register an ARR instance
// If the instance already exists (by URL), it returns nil without error
// Also creates the appropriate category in SABnzbd configuration based on ARR type:
// - Radarr instances get "movies" category
// - Sonarr instances get "tv" category
func (s *Service) RegisterInstance(ctx context.Context, arrURL, apiKey string) error {
	if s.configManager == nil {
		return fmt.Errorf("config manager not available")
	}

	slog.InfoContext(ctx, "Attempting to register ARR instance", "url", arrURL)

	// Check if instance already exists
	if s.instanceExistsByURL(arrURL) {
		slog.DebugContext(ctx, "ARR instance already exists, skipping registration", "url", arrURL)
		return nil
	}

	// Detect ARR type
	arrType, err := s.detectARRType(ctx, arrURL, apiKey)
	if err != nil {
		return fmt.Errorf("failed to detect ARR type: %w", err)
	}

	// Determine category based on ARR type
	var category string
	switch arrType {
	case "radarr":
		category = "movies"
	case "sonarr":
		category = "tv"
	default:
		return fmt.Errorf("unsupported ARR type: %s", arrType)
	}

	// Generate instance name
	instanceName, err := s.generateInstanceName(arrURL)
	if err != nil {
		return fmt.Errorf("failed to generate instance name: %w", err)
	}

	slog.InfoContext(ctx, "Registering new ARR instance",
		"name", instanceName,
		"type", arrType,
		"url", arrURL,
		"category", category)

	// Get current config and make a deep copy
	currentConfig := s.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy()

	// Create new instance config
	enabled := true
	newInstance := config.ArrsInstanceConfig{
		Name:    instanceName,
		URL:     arrURL,
		APIKey:  apiKey,
		Enabled: &enabled,
	}

	// Add to appropriate array
	switch arrType {
	case "radarr":
		newConfig.Arrs.RadarrInstances = append(newConfig.Arrs.RadarrInstances, newInstance)
	case "sonarr":
		newConfig.Arrs.SonarrInstances = append(newConfig.Arrs.SonarrInstances, newInstance)
	}

	// Create category for this ARR type
	s.ensureCategoryExistsInConfig(ctx, newConfig, category)

	// Update and save configuration
	if err := s.configManager.UpdateConfig(newConfig); err != nil {
		return fmt.Errorf("failed to update configuration: %w", err)
	}

	if err := s.configManager.SaveConfig(); err != nil {
		return fmt.Errorf("failed to save configuration: %w", err)
	}

	slog.InfoContext(ctx, "Successfully registered ARR instance",
		"name", instanceName,
		"type", arrType,
		"url", arrURL,
		"category", category)

	return nil
}

// ensureCategoryExistsInConfig ensures a category exists in the provided config
func (s *Service) ensureCategoryExistsInConfig(ctx context.Context, cfg *config.Config, category string) {
	// Use default category if empty
	if category == "" {
		category = "default"
	}

	// Check if category already exists
	for _, existingCategory := range cfg.SABnzbd.Categories {
		if existingCategory.Name == category {
			slog.DebugContext(ctx, "Category already exists, skipping creation", "category", category)
			return
		}
	}

	// Calculate next order number
	nextOrder := 0
	for _, existingCategory := range cfg.SABnzbd.Categories {
		if existingCategory.Order >= nextOrder {
			nextOrder = existingCategory.Order + 1
		}
	}

	// Create new category with default values
	newCategory := config.SABnzbdCategory{
		Name:     category,
		Order:    nextOrder,
		Priority: 0,
		Dir:      category, // Use category name as directory
	}

	cfg.SABnzbd.Categories = append(cfg.SABnzbd.Categories, newCategory)

	slog.InfoContext(ctx, "Created new category",
		"category", category,
		"order", nextOrder,
		"dir", category)
}

// GetInstance returns a specific instance by type and name
func (s *Service) GetInstance(instanceType, instanceName string) *ConfigInstance {
	instances := s.getConfigInstances()
	for _, instance := range instances {
		if instance.Type == instanceType && instance.Name == instanceName {
			return instance
		}
	}
	return nil
}

// TestConnection tests the connection to an arrs instance
func (s *Service) TestConnection(ctx context.Context, instanceType, url, apiKey string) error {
	switch instanceType {
	case "radarr":
		client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatusContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to Radarr: %w", err)
		}
		return nil

	case "sonarr":
		client := sonarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatus()
		if err != nil {
			return fmt.Errorf("failed to connect to Sonarr: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported instance type: %s", instanceType)
	}
}

// blocklistRadarrMovieFile finds the history event for the given file and marks it as failed (blocklisting the release)
func (s *Service) blocklistRadarrMovieFile(ctx context.Context, client *radarr.Radarr, movieID int64, fileID int64) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for movie file", "movie_id", movieID, "file_id", fileID)

	req := &starr.PageReq{
		PageSize: 100,
		SortKey:  "date",
		SortDir:  starr.SortDescend,
	}
	req.Set("movieId", strconv.FormatInt(movieID, 10))

	history, err := client.GetHistoryPageContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	targetFileID := strconv.FormatInt(fileID, 10)

	for _, record := range history.Records {
		// Check if this history record corresponds to our file
		if record.Data.FileID == targetFileID {
			slog.InfoContext(ctx, "Found history record for file, marking as failed to blocklist release",
				"history_id", record.ID,
				"source_title", record.SourceTitle,
				"event_type", record.EventType)

			// Mark history as failed (this adds to blocklist)
			if err := client.FailContext(ctx, record.ID); err != nil {
				return fmt.Errorf("failed to mark history as failed: %w", err)
			}
			return nil
		}
	}

	slog.WarnContext(ctx, "No history record found for file, cannot blocklist", "movie_id", movieID, "file_id", fileID)
	return nil
}

// blocklistSonarrEpisodeFile finds the history event for the given file and marks it as failed (blocklisting the release)
func (s *Service) blocklistSonarrEpisodeFile(ctx context.Context, client *sonarr.Sonarr, seriesID int64, fileID int64) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for episode file", "series_id", seriesID, "file_id", fileID)

	req := &starr.PageReq{
		PageSize: 100,
		SortKey:  "date",
		SortDir:  starr.SortDescend,
	}
	req.Set("seriesId", strconv.FormatInt(seriesID, 10))

	history, err := client.GetHistoryPageContext(ctx, req)
	if err != nil {
		return fmt.Errorf("failed to get history: %w", err)
	}

	targetFileID := strconv.FormatInt(fileID, 10)

	for _, record := range history.Records {
		// Check if this history record corresponds to our file
		if record.Data.FileID == targetFileID {
			slog.InfoContext(ctx, "Found history record for file, marking as failed to blocklist release",
				"history_id", record.ID,
				"source_title", record.SourceTitle,
				"event_type", record.EventType)

			// Mark history as failed (this adds to blocklist)
			if err := client.FailContext(ctx, record.ID); err != nil {
				return fmt.Errorf("failed to mark history as failed: %w", err)
			}
			return nil
		}
	}

	slog.WarnContext(ctx, "No history record found for file, cannot blocklist", "series_id", seriesID, "file_id", fileID)
	return nil
}

