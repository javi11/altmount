package arrs

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

// Regex to extract Season and Episode numbers (e.g., S01E05, s1e5)
var seasonEpisodeRegex = regexp.MustCompile(`(?i)S(\d{1,2})E(\d{1,3})`)

// parseSeasonEpisode extracts Season and Episode numbers from a file path.
// Returns season and episode as integers, or -1, -1 if not found.
func parseSeasonEpisode(filePath string) (int, int) {
	matches := seasonEpisodeRegex.FindStringSubmatch(filePath)
	if len(matches) == 3 {
		var season, episode int
		fmt.Sscanf(matches[1], "%d", &season)
		fmt.Sscanf(matches[2], "%d", &episode)
		return season, episode
	}
	return -1, -1
}

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
}

// NewService creates a new arrs service for health monitoring and file repair
func NewService(configGetter config.ConfigGetter, configManager ConfigManager) *Service {
	return &Service{
		configGetter:  configGetter,
		configManager: configManager,
		radarrClients: make(map[string]*radarr.Radarr),
		sonarrClients: make(map[string]*sonarr.Sonarr),
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
func (s *Service) findInstanceForFilePath(ctx context.Context, filePath string) (instanceType string, instanceName string, err error) {
	slog.DebugContext(ctx, "Finding instance for file path", "file_path", filePath)

	// Try each enabled ARR instance to see which one manages this file
	for _, instance := range s.getConfigInstances() {
		if !instance.Enabled {
			continue
		}

		slog.DebugContext(ctx, "Checking instance for file",
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

	return "", "", fmt.Errorf("no ARR instance found managing file path: %s", filePath)
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
// The pathForRescan should be the library path (symlink or .strm file) if available,
// otherwise the mount path. It's the caller's responsibility to find the appropriate path.
func (s *Service) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string) error {
	slog.InfoContext(ctx, "Triggering ARR rescan", "path", pathForRescan, "relative_path", relativePath)

	// Find which ARR instance manages this file path
	instanceType, instanceName, err := s.findInstanceForFilePath(ctx, pathForRescan)
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
	movies, err := client.GetMovieContext(ctx, &radarr.GetMovie{})
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
	response, err := client.SendCommandContext(ctx, &radarr.CommandRequest{
		Name:     "RescanMovie",
		MovieIDs: []int64{targetMovie.ID},
	})
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr rescan for movie ID %d: %w", targetMovie.ID, err)
	}

	slog.DebugContext(ctx, "Successfully triggered Radarr rescan",
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
	series, err := client.GetAllSeriesContext(ctx)
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

	// If we couldn't find the file (or episodes linked to it), try to find episode by SxxEyy
	if len(episodeIDs) == 0 {
		slog.InfoContext(ctx, "Episode file not found in Sonarr, attempting to identify episode by SxxEyy",
			"file_path", filePath)

		season, episodeNum := parseSeasonEpisode(relativePath)
		if season != -1 && episodeNum != -1 {
			for _, episode := range episodes {
				if episode.SeasonNumber == season && episode.EpisodeNumber == episodeNum {
					episodeIDs = append(episodeIDs, episode.ID)
					slog.InfoContext(ctx, "Found matching episode by Season/Episode number",
						"season", season,
						"episode", episodeNum,
						"episode_id", episode.ID)
					break
				}
			}
		}
	}

	if len(episodeIDs) == 0 {
		return fmt.Errorf("no episodes found for file: %s (path match failed, SxxEyy match failed)", filePath)
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
