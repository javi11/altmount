package arrs

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

// SyncStatus represents the status of a sync operation
type SyncStatus string

const (
	SyncStatusRunning    SyncStatus = "running"
	SyncStatusCompleted  SyncStatus = "completed"
	SyncStatusFailed     SyncStatus = "failed"
	SyncStatusCancelling SyncStatus = "cancelling"
	SyncStatusCancelled  SyncStatus = "cancelled"
)

// SyncProgressInfo contains detailed progress information
type SyncProgressInfo struct {
	ProcessedCount int    `json:"processed_count"`
	ErrorCount     int    `json:"error_count"`
	TotalItems     *int   `json:"total_items,omitempty"`
	CurrentBatch   string `json:"current_batch,omitempty"`
}

// SyncProgress tracks the progress of a sync operation
type SyncProgress struct {
	InstanceType string             `json:"instance_type"`
	InstanceName string             `json:"instance_name"`
	Status       SyncStatus         `json:"status"`
	StartedAt    time.Time          `json:"started_at"`
	Progress     SyncProgressInfo   `json:"progress"`
	Cancel       context.CancelFunc `json:"-"` // Not serialized
}

// SyncResult contains the final result of a sync operation
type SyncResult struct {
	InstanceType   string       `json:"instance_type"`
	InstanceName   string       `json:"instance_name"`
	CompletedAt    time.Time    `json:"completed_at"`
	Status         SyncStatus   `json:"status"`
	ProcessedCount int          `json:"processed_count"`
	ErrorCount     int          `json:"error_count"`
	ErrorMessage   *string      `json:"error_message,omitempty"`
}

// ArrsInstanceState holds runtime state for an arrs instance
type ArrsInstanceState struct {
	LastSyncAt     *time.Time    `json:"last_sync_at"`
	LastSyncResult *SyncResult   `json:"last_result"`
	ActiveStatus   *SyncProgress `json:"active_status"`
}

// ConfigInstance represents an arrs instance from configuration
type ConfigInstance struct {
	Name                string `json:"name"`
	Type                string `json:"type"` // "radarr" or "sonarr"
	URL                 string `json:"url"`
	APIKey              string `json:"api_key"`
	Enabled             bool   `json:"enabled"`
	SyncIntervalHours   int    `json:"sync_interval_hours"`
}

// Service manages syncing with Radarr and Sonarr instances using configuration-first approach
type Service struct {
	configGetter  config.ConfigGetter
	mediaRepo     *database.MediaRepository
	logger        *slog.Logger
	ctx           context.Context
	cancel        context.CancelFunc
	wg            sync.WaitGroup
	mu            sync.RWMutex
	radarrClients map[string]*radarr.Radarr // key: instance name
	sonarrClients map[string]*sonarr.Sonarr // key: instance name
	running       bool

	// In-memory state tracking
	stateMutex    sync.RWMutex
	instanceState map[string]*ArrsInstanceState // key: "type:name"
}

// NewService creates a new arrs service with configuration-first approach
func NewService(configGetter config.ConfigGetter, mediaRepo *database.MediaRepository, logger *slog.Logger) *Service {
	ctx, cancel := context.WithCancel(context.Background())
	return &Service{
		configGetter:  configGetter,
		mediaRepo:     mediaRepo,
		logger:        logger,
		ctx:           ctx,
		cancel:        cancel,
		radarrClients: make(map[string]*radarr.Radarr),
		sonarrClients: make(map[string]*sonarr.Sonarr),
		instanceState: make(map[string]*ArrsInstanceState),
	}
}

// getInstanceKey generates a unique key for an instance
func getInstanceKey(instanceType, instanceName string) string {
	return instanceType + ":" + instanceName
}

// stripMountPath strips the mount path from a file path and ensures the result starts with /
func stripMountPath(filePath, mountPath string) string {
	if mountPath == "" {
		return filePath
	}

	// Remove the mount path prefix
	stripped := strings.TrimPrefix(filePath, mountPath)

	// Ensure the result starts with /
	if !strings.HasPrefix(stripped, "/") {
		stripped = "/" + stripped
	}

	return stripped
}

// getConfigInstances returns all arrs instances from current configuration
func (s *Service) getConfigInstances() []*ConfigInstance {
	cfg := s.configGetter()
	instances := make([]*ConfigInstance, 0)

	// Convert Radarr instances
	if len(cfg.Arrs.RadarrInstances) > 0 {
		for _, radarrConfig := range cfg.Arrs.RadarrInstances {
			syncInterval := 24 // default
			if radarrConfig.SyncIntervalHours != nil {
				syncInterval = *radarrConfig.SyncIntervalHours
			}

			instance := &ConfigInstance{
				Name:                radarrConfig.Name,
				Type:                "radarr",
				URL:                 radarrConfig.URL,
				APIKey:              radarrConfig.APIKey,
				Enabled:             radarrConfig.Enabled != nil && *radarrConfig.Enabled,
				SyncIntervalHours:   syncInterval,
			}
			instances = append(instances, instance)
		}
	}

	// Convert Sonarr instances
	if len(cfg.Arrs.SonarrInstances) > 0 {
		for _, sonarrConfig := range cfg.Arrs.SonarrInstances {
			syncInterval := 24 // default
			if sonarrConfig.SyncIntervalHours != nil {
				syncInterval = *sonarrConfig.SyncIntervalHours
			}

			instance := &ConfigInstance{
				Name:                sonarrConfig.Name,
				Type:                "sonarr",
				URL:                 sonarrConfig.URL,
				APIKey:              sonarrConfig.APIKey,
				Enabled:             sonarrConfig.Enabled != nil && *sonarrConfig.Enabled,
				SyncIntervalHours:   syncInterval,
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

// getOrCreateInstanceState gets or creates state for an instance
func (s *Service) getOrCreateInstanceState(instanceType, instanceName string) *ArrsInstanceState {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()

	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists {
		state = &ArrsInstanceState{}
		s.instanceState[key] = state
	}

	return state
}

// Start starts the arrs service
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("arrs service is already running")
	}

	s.running = true
	s.wg.Add(1)
	go s.serviceLoop()

	s.logger.Info("Arrs service started")
	return nil
}

// Stop stops the arrs service
func (s *Service) Stop() error {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return nil
	}
	s.running = false
	s.mu.Unlock()

	s.cancel()
	s.wg.Wait()

	// Close all clients
	s.mu.Lock()
	defer s.mu.Unlock()

	for name, client := range s.radarrClients {
		if client != nil {
			s.logger.Debug("Closing Radarr client", "instance", name)
		}
		delete(s.radarrClients, name)
	}

	for name, client := range s.sonarrClients {
		if client != nil {
			s.logger.Debug("Closing Sonarr client", "instance", name)
		}
		delete(s.sonarrClients, name)
	}

	s.logger.Info("Arrs service stopped")
	return nil
}

// serviceLoop runs the main service loop
func (s *Service) serviceLoop() {
	defer s.wg.Done()

	// For now, we only support manual sync
	// Scheduled sync can be added later if needed

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// Check if we should do any scheduled work
			s.performSync()
		}
	}
}

// performSync checks for instances that need syncing and syncs them
func (s *Service) performSync() {
	for _, instance := range s.getConfigInstances() {
		if instance.Enabled {
			if err := s.TriggerSync(instance.Type, instance.Name); err != nil {
				s.logger.Error("Failed to trigger sync", "instance", instance.Name, "type", instance.Type, "error", err)
			}
		}
	}
}

// TriggerSync triggers a manual sync for a specific instance by type and name
func (s *Service) TriggerSync(instanceType, instanceName string) error {
	// Find the instance in configuration
	instance, err := s.findConfigInstance(instanceType, instanceName)
	if err != nil {
		return fmt.Errorf("instance not found in configuration: %w", err)
	}

	if !instance.Enabled {
		return fmt.Errorf("instance is disabled: %s/%s", instanceType, instanceName)
	}

	// Start syncing in a goroutine
	go s.syncConfigInstance(instance)

	return nil
}

// syncConfigInstance performs syncing for a configuration-based instance
func (s *Service) syncConfigInstance(instance *ConfigInstance) {
	s.logger.Info("Starting manual sync", "instance", instance.Name, "type", instance.Type)

	// Get or create state for this instance
	state := s.getOrCreateInstanceState(instance.Type, instance.Name)

	// Check if already syncing this instance (only block if actively running)
	s.stateMutex.RLock()
	if state.ActiveStatus != nil && (state.ActiveStatus.Status == SyncStatusRunning || state.ActiveStatus.Status == SyncStatusCancelling) {
		s.stateMutex.RUnlock()
		s.logger.Warn("Instance is already being synced", "instance", instance.Name, "type", instance.Type)
		return
	}
	s.stateMutex.RUnlock()

	// Create cancellation context for this sync
	_, cancel := context.WithCancel(s.ctx)
	defer cancel()

	// Initialize progress tracking
	progress := &SyncProgress{
		InstanceType: instance.Type,
		InstanceName: instance.Name,
		Status:       SyncStatusRunning,
		StartedAt:    time.Now(),
		Progress: SyncProgressInfo{
			ProcessedCount: 0,
			ErrorCount:     0,
			CurrentBatch:   "initializing",
		},
		Cancel: cancel,
	}

	// Register active syncing
	s.stateMutex.Lock()
	state.ActiveStatus = progress
	s.stateMutex.Unlock()

	// Ensure cleanup on exit - preserve final status for visibility
	defer func() {
		if progress.Status == SyncStatusRunning || progress.Status == SyncStatusCancelling {
			// Only clear status if we're still in a transient state
			s.stateMutex.Lock()
			state.ActiveStatus = nil
			s.stateMutex.Unlock()
		}
		// For completed/failed states, keep ActiveStatus so frontend can see the final result
	}()

	var err error
	if instance.Type == "radarr" {
		err = s.syncRadarrConfig(instance, progress)
	} else if instance.Type == "sonarr" {
		err = s.syncSonarrConfig(instance, progress)
	} else {
		err = fmt.Errorf("unsupported instance type: %s", instance.Type)
	}

	// Update final status
	if err != nil {
		progress.Status = SyncStatusFailed
		s.logger.Error("Sync failed", "instance", instance.Name, "type", instance.Type, "error", err)
	} else {
		progress.Status = SyncStatusCompleted
		s.logger.Info("Sync completed", "instance", instance.Name, "type", instance.Type)
	}

	// Record the final result
	result := &SyncResult{
		InstanceType:   instance.Type,
		InstanceName:   instance.Name,
		CompletedAt:    time.Now(),
		Status:         progress.Status,
		ProcessedCount: progress.Progress.ProcessedCount,
		ErrorCount:     progress.Progress.ErrorCount,
	}

	if err != nil {
		errMsg := err.Error()
		result.ErrorMessage = &errMsg
	}

	// Store the result in state
	s.stateMutex.Lock()
	state.LastSyncAt = &result.CompletedAt
	state.LastSyncResult = result
	s.stateMutex.Unlock()

	duration := time.Since(progress.StartedAt)
	s.logger.Info("Sync completed",
		"instance", instance.Name,
		"type", instance.Type,
		"duration", duration)
}

// syncRadarrConfig syncs a Radarr instance using configuration
func (s *Service) syncRadarrConfig(instance *ConfigInstance, progress *SyncProgress) error {
	// Get configuration for mount path
	cfg := s.configGetter()
	mountPath := cfg.Arrs.MountPath

	// Get or create Radarr client
	client, err := s.getOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return fmt.Errorf("failed to get Radarr client: %w", err)
	}

	// Update progress: fetching movies
	progress.Progress.CurrentBatch = "fetching movies from Radarr"

	// Get all movies from Radarr
	s.logger.Debug("Fetching movies from Radarr", "instance", instance.Name)
	movies, err := client.GetMovie(&radarr.GetMovie{})
	if err != nil {
		return fmt.Errorf("failed to get movies from Radarr: %w", err)
	}

	// Filter movies with files and extract file information
	var mediaFiles []database.MediaFileInput
	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil {
			// Convert file size from int to *int64
			var fileSize *int64
			if movie.MovieFile.Size > 0 {
				size := int64(movie.MovieFile.Size)
				fileSize = &size
			}

			// Strip mount path from file path
			originalPath := movie.MovieFile.Path
			strippedPath := stripMountPath(originalPath, mountPath)

			// Log if path doesn't match mount path prefix (potential issue)
			if mountPath != "" && !strings.HasPrefix(originalPath, mountPath) {
				s.logger.Warn("Movie file path does not start with mount path",
					"instance", instance.Name,
					"movie_title", movie.Title,
					"file_path", originalPath,
					"mount_path", mountPath)
			}

			mediaFile := database.MediaFileInput{
				InstanceName: instance.Name,
				InstanceType: "radarr",
				ExternalID:   int64(movie.ID),
				FilePath:     strippedPath,
				FileSize:     fileSize,
			}
			mediaFiles = append(mediaFiles, mediaFile)
		}
	}

	s.logger.Info("Processing Radarr movies",
		"instance", instance.Name,
		"total_movies", len(movies),
		"movies_with_files", len(mediaFiles))

	// Update progress
	totalMovies := len(mediaFiles)
	progress.Progress.TotalItems = &totalMovies
	progress.Progress.CurrentBatch = "storing movies in database"

	// Store media files in database if we have a repository
	if s.mediaRepo != nil && len(mediaFiles) > 0 {
		result, err := s.mediaRepo.SyncMediaFiles(instance.Name, "radarr", mediaFiles)
		if err != nil {
			s.logger.Error("Failed to sync media files to database",
				"instance", instance.Name,
				"error", err)
			return fmt.Errorf("failed to sync media files: %w", err)
		}

		s.logger.Info("Synced Radarr media files to database",
			"instance", instance.Name,
			"added", result.Added,
			"updated", result.Updated,
			"removed", result.Removed)
	}

	progress.Progress.ProcessedCount = len(mediaFiles)

	return nil
}

// syncSonarrConfig syncs a Sonarr instance using configuration
func (s *Service) syncSonarrConfig(instance *ConfigInstance, progress *SyncProgress) error {
	// Get configuration for mount path
	cfg := s.configGetter()
	mountPath := cfg.Arrs.MountPath

	// Get or create Sonarr client
	client, err := s.getOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr client: %w", err)
	}

	// Update progress: fetching series list
	progress.Progress.CurrentBatch = "fetching series list from Sonarr"

	series, err := client.GetAllSeries()
	if err != nil {
		return fmt.Errorf("failed to get series from Sonarr: %w", err)
	}

	// Set up progress tracking based on show count
	totalShows := len(series)
	progress.Progress.TotalItems = &totalShows
	progress.Progress.ProcessedCount = 0

	s.logger.Info("Processing Sonarr series",
		"instance", instance.Name,
		"total_shows", totalShows)

	// Collect all media files from all shows
	var allMediaFiles []database.MediaFileInput
	processedShows := 0

	for _, show := range series {
		processedShows++

		// Update progress to show current show being processed
		progress.Progress.ProcessedCount = processedShows
		progress.Progress.CurrentBatch = fmt.Sprintf("processing show %d/%d: %s", processedShows, totalShows, show.Title)

		// Get all episode files from Sonarr - this gives us the actual files
		s.logger.Debug("Fetching episode files from Sonarr",
			"instance", instance.Name,
			"show", show.Title,
			"show_id", show.ID)

		episodeFiles, err := client.GetSeriesEpisodeFiles(show.ID)
		if err != nil {
			s.logger.Error("Failed to get episode files for show",
				"instance", instance.Name,
				"show", show.Title,
				"error", err)
			continue // Skip this show but continue with others
		}

		s.logger.Debug("Found episode files for show",
			"instance", instance.Name,
			"show", show.Title,
			"episode_files_count", len(episodeFiles))

		// Extract file information for this show
		for _, episodeFile := range episodeFiles {
			// Convert file size from int64 to *int64
			var fileSize *int64
			if episodeFile.Size > 0 {
				fileSize = &episodeFile.Size
			}

			// Strip mount path from file path
			originalPath := episodeFile.Path
			strippedPath := stripMountPath(originalPath, mountPath)

			// Log if path doesn't match mount path prefix (potential issue)
			if mountPath != "" && !strings.HasPrefix(originalPath, mountPath) {
				s.logger.Warn("Episode file path does not start with mount path",
					"instance", instance.Name,
					"show_title", show.Title,
					"file_path", originalPath,
					"mount_path", mountPath)
			}

			mediaFile := database.MediaFileInput{
				InstanceName: instance.Name,
				InstanceType: "sonarr",
				ExternalID:   int64(episodeFile.ID),
				FilePath:     strippedPath,
				FileSize:     fileSize,
			}
			allMediaFiles = append(allMediaFiles, mediaFile)
		}
	}

	// Update progress for database sync
	progress.Progress.CurrentBatch = fmt.Sprintf("storing %d episode files in database", len(allMediaFiles))

	// Store all media files in database if we have a repository
	if s.mediaRepo != nil && len(allMediaFiles) > 0 {
		result, err := s.mediaRepo.SyncMediaFiles(instance.Name, "sonarr", allMediaFiles)
		if err != nil {
			s.logger.Error("Failed to sync media files to database",
				"instance", instance.Name,
				"error", err)
			return fmt.Errorf("failed to sync media files: %w", err)
		}

		s.logger.Info("Synced Sonarr media files to database",
			"instance", instance.Name,
			"total_shows", totalShows,
			"total_episode_files", len(allMediaFiles),
			"added", result.Added,
			"updated", result.Updated,
			"removed", result.Removed)
	}

	return nil
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

// GetSyncStatus returns the current sync status for an instance by type and name
func (s *Service) GetSyncStatus(instanceType, instanceName string) (*SyncProgress, error) {
	// Verify instance exists in configuration
	_, err := s.findConfigInstance(instanceType, instanceName)
	if err != nil {
		return nil, err
	}

	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()

	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists {
		return nil, nil // No status available
	}

	return state.ActiveStatus, nil
}

// GetLastSyncResult returns the last sync result for an instance by type and name
func (s *Service) GetLastSyncResult(instanceType, instanceName string) (*SyncResult, error) {
	// Verify instance exists in configuration
	_, err := s.findConfigInstance(instanceType, instanceName)
	if err != nil {
		return nil, err
	}

	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()

	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists {
		return nil, nil // No result available
	}

	return state.LastSyncResult, nil
}

// GetAllActiveSyncs returns all currently active syncs
func (s *Service) GetAllActiveSyncs() []*SyncProgress {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()

	var activeSyncs []*SyncProgress
	for _, state := range s.instanceState {
		if state.ActiveStatus != nil {
			activeSyncs = append(activeSyncs, state.ActiveStatus)
		}
	}

	return activeSyncs
}

// GetAllInstances returns all arrs instances from configuration with their state
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

// CancelSync cancels an active sync by type and name
func (s *Service) CancelSync(instanceType, instanceName string) error {
	s.stateMutex.RLock()
	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists || state.ActiveStatus == nil {
		s.stateMutex.RUnlock()
		return fmt.Errorf("no active sync found for instance: %s/%s", instanceType, instanceName)
	}

	progress := state.ActiveStatus
	s.stateMutex.RUnlock()

	// Cancel the operation
	if progress.Cancel != nil {
		progress.Cancel()
		progress.Status = SyncStatusCancelling
		s.logger.Info("Cancelled sync", "instance", instanceName, "type", instanceType)
		return nil
	}

	return fmt.Errorf("cannot cancel sync - no cancellation context available")
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
