package arrs

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"

	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

// ConfigInstance represents an arrs instance from configuration
type ConfigInstance struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // "radarr" or "sonarr"
	URL     string `json:"url"`
	APIKey  string `json:"api_key"`
	Enabled bool   `json:"enabled"`
}

// Service manages Radarr and Sonarr instances for health monitoring and file repair
type Service struct {
	configGetter  config.ConfigGetter
	logger        *slog.Logger
	mu            sync.RWMutex
	radarrClients map[string]*radarr.Radarr // key: instance name
	sonarrClients map[string]*sonarr.Sonarr // key: instance name
}

// NewService creates a new arrs service for health monitoring and file repair
func NewService(configGetter config.ConfigGetter, logger *slog.Logger) *Service {
	return &Service{
		configGetter:  configGetter,
		logger:        logger,
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
func (s *Service) findInstanceForFilePath(filePath string) (instanceType string, instanceName string, err error) {
	cfg := s.configGetter()
	mountPath := cfg.MountPath

	// Add mount path to file path to get full path for ARR APIs
	fullPath := filePath
	if mountPath != "" {
		fullPath = filepath.Join(mountPath, filePath)
	}

	slog.Debug("Finding instance for file path", "file_path", filePath, "full_path", fullPath, "mount_path", mountPath)

	// Try each enabled ARR instance to see which one manages this file
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
			if s.radarrManagesFile(client, fullPath) {
				return "radarr", instance.Name, nil
			}

		case "sonarr":
			client, err := s.getOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				continue
			}
			if s.sonarrManagesFile(client, fullPath) {
				return "sonarr", instance.Name, nil
			}
		}
	}

	return "", "", fmt.Errorf("no ARR instance found managing file path: %s", filePath)
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
func (s *Service) TriggerFileRescan(ctx context.Context, filePath string) error {
	// Find which ARR instance manages this file path
	instanceType, instanceName, err := s.findInstanceForFilePath(filePath)
	if err != nil {
		return fmt.Errorf("failed to find ARR instance for file path %s: %w", filePath, err)
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
		return s.triggerRadarrRescanByPath(ctx, client, filePath, instanceName)

	case "sonarr":
		client, err := s.getOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
		if err != nil {
			return fmt.Errorf("failed to create Sonarr client: %w", err)
		}
		return s.triggerSonarrRescanByPath(ctx, client, filePath, instanceName)

	default:
		return fmt.Errorf("unsupported instance type: %s", instanceType)
	}
}

// radarrManagesFile checks if Radarr manages the given file path using root folders (checkrr approach)
func (s *Service) radarrManagesFile(client *radarr.Radarr, filePath string) bool {
	s.logger.Debug("Checking Radarr root folders for file ownership",
		"file_path", filePath)

	// Get root folders from Radarr (much faster than GetMovie)
	rootFolders, err := client.GetRootFolders()
	if err != nil {
		s.logger.Debug("Failed to get root folders from Radarr for file check", "error", err)
		return false
	}

	// Check if file path starts with any root folder path
	for _, folder := range rootFolders {
		s.logger.Debug("Checking Radarr root folder", "folder_path", folder.Path, "file_path", filePath)
		if strings.HasPrefix(filePath, folder.Path) {
			s.logger.Debug("File matches Radarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	s.logger.Debug("File does not match any Radarr root folders")
	return false
}

// triggerRadarrRescanByPath triggers a rescan in Radarr for the given file path
func (s *Service) triggerRadarrRescanByPath(ctx context.Context, client *radarr.Radarr, filePath, instanceName string) error {
	cfg := s.configGetter()
	mountPath := cfg.MountPath

	// Add mount path to get full path for Radarr API
	fullPath := filePath
	if mountPath != "" {
		fullPath = filepath.Join(mountPath, strings.TrimPrefix(filePath, "/"))
	}

	s.logger.DebugContext(ctx, "Checking Radarr for file path",
		"instance", instanceName,
		"file_path", filePath,
		"full_path", fullPath)

	// Get all movies to find the one with matching file path
	movies, err := client.GetMovie(&radarr.GetMovie{})
	if err != nil {
		return fmt.Errorf("failed to get movies from Radarr: %w", err)
	}

	var targetMovie *radarr.Movie
	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil && movie.MovieFile.Path == fullPath {
			targetMovie = movie
			break
		}
	}

	if targetMovie == nil {
		return fmt.Errorf("no movie found with file path: %s. Check if the movie has any files", fullPath)
	}

	s.logger.DebugContext(ctx, "Found matching movie for file",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"movie_title", targetMovie.Title,
		"movie_path", targetMovie.Path,
		"file_path", fullPath)

	// Delete the existing file
	err = client.DeleteMovieFilesContext(ctx, targetMovie.MovieFile.ID)
	if err != nil {
		s.logger.Warn("Failed to delete movie file, continuing with rescan",
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

	s.logger.DebugContext(ctx, "Successfully triggered Radarr rescan",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"command_id", response.ID)

	return nil
}

// sonarrManagesFile checks if Sonarr manages the given file path using root folders (checkrr approach)
func (s *Service) sonarrManagesFile(client *sonarr.Sonarr, filePath string) bool {
	s.logger.Debug("Checking Sonarr root folders for file ownership",
		"file_path", filePath)

	// Get root folders from Sonarr (much faster than GetAllSeries)
	rootFolders, err := client.GetRootFolders()
	if err != nil {
		s.logger.Debug("Failed to get root folders from Sonarr for file check", "error", err)
		return false
	}

	// Check if file path starts with any root folder path
	for _, folder := range rootFolders {
		s.logger.Debug("Checking Sonarr root folder", "folder_path", folder.Path, "file_path", filePath)
		if strings.HasPrefix(filePath, folder.Path) {
			s.logger.Debug("File matches Sonarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	s.logger.Debug("File does not match any Sonarr root folders")
	return false
}

// triggerSonarrRescanByPath triggers a rescan in Sonarr for the given file path
func (s *Service) triggerSonarrRescanByPath(ctx context.Context, client *sonarr.Sonarr, filePath, instanceName string) error {
	cfg := s.configGetter()
	mountPath := cfg.MountPath

	// Add mount path to get full path for Sonarr API
	fullPath := filePath
	if mountPath != "" {
		fullPath = filepath.Join(mountPath, strings.TrimPrefix(filePath, "/"))
	}

	s.logger.DebugContext(ctx, "Triggering Sonarr rescan/re-download by path",
		"instance", instanceName,
		"file_path", filePath,
		"full_path", fullPath)

	// Get all series to find the one that contains this file path
	series, err := client.GetAllSeries()
	if err != nil {
		return fmt.Errorf("failed to get series from Sonarr: %w", err)
	}

	// Find the series that contains this file path
	var targetSeries *sonarr.Series
	for _, show := range series {
		if strings.Contains(fullPath, show.Path) {
			targetSeries = show
			break
		}
	}

	if targetSeries == nil {
		return fmt.Errorf("no series found containing file path: %s", fullPath)
	}

	s.logger.Debug("Found matching series for file",
		"series_title", targetSeries.Title,
		"series_path", targetSeries.Path,
		"file_path", fullPath)

	// Get all episodes for this specific series
	episodes, err := client.GetSeriesEpisodes(&sonarr.GetEpisode{
		SeriesID: targetSeries.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", targetSeries.Title, err)
	}

	// Get episode files for this series to find the matching file
	episodeFiles, err := client.GetSeriesEpisodeFiles(targetSeries.ID)
	if err != nil {
		return fmt.Errorf("failed to get episode files for series %s: %w", targetSeries.Title, err)
	}

	// Find the episode file with matching path
	var targetEpisodeFile *sonarr.EpisodeFile
	for _, episodeFile := range episodeFiles {
		if episodeFile.Path == fullPath {
			targetEpisodeFile = episodeFile
			break
		}
	}

	if targetEpisodeFile == nil {
		return fmt.Errorf("no episode file found with path: %s", fullPath)
	}

	// Find episodes that use this episode file
	var targetEpisodes []*sonarr.Episode
	for _, episode := range episodes {
		if episode.HasFile && episode.EpisodeFileID == targetEpisodeFile.ID {
			targetEpisodes = append(targetEpisodes, episode)
		}
	}

	if len(targetEpisodes) == 0 {
		return fmt.Errorf("no episodes found with file path: %s", fullPath)
	}

	s.logger.Debug("Found matching episodes",
		"episode_count", len(targetEpisodes),
		"episode_file_id", targetEpisodeFile.ID)

	// Delete the existing episode file
	err = client.DeleteEpisodeFileContext(ctx, targetEpisodeFile.ID)
	if err != nil {
		s.logger.Warn("Failed to delete episode file",
			"instance", instanceName,
			"episode_file_id", targetEpisodeFile.ID,
			"error", err)
	}

	// Trigger episode search for all episodes in this file
	var episodeIDs []int64
	for _, episode := range targetEpisodes {
		episodeIDs = append(episodeIDs, episode.ID)
	}

	searchCmd := &sonarr.CommandRequest{
		Name:       "EpisodeSearch",
		EpisodeIDs: episodeIDs,
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger episode search: %w", err)
	}

	s.logger.DebugContext(ctx, "Successfully triggered episode search for re-download",
		"instance", instanceName,
		"series_title", targetSeries.Title,
		"episode_ids", episodeIDs,
		"command_id", response.ID)

	return nil
}

// GetAllInstances returns all arrs instances from configuration
func (s *Service) GetAllInstances() []*ConfigInstance {
	return s.getConfigInstances()
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
func (s *Service) TestConnection(instanceType, url, apiKey string) error {
	switch instanceType {
	case "radarr":
		client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatus()
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
