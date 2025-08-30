package scraper

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

// ScrapeStatus represents the status of a scraping operation
type ScrapeStatus string

const (
	ScrapeStatusRunning    ScrapeStatus = "running"
	ScrapeStatusCompleted  ScrapeStatus = "completed"
	ScrapeStatusFailed     ScrapeStatus = "failed"
	ScrapeStatusCancelling ScrapeStatus = "cancelling"
	ScrapeStatusCancelled  ScrapeStatus = "cancelled"
)

// ScrapeProgressInfo contains detailed progress information
type ScrapeProgressInfo struct {
	ProcessedCount int    `json:"processed_count"`
	ErrorCount     int    `json:"error_count"`
	TotalItems     *int   `json:"total_items,omitempty"`
	CurrentBatch   string `json:"current_batch,omitempty"`
}

// ScrapeProgress tracks the progress of a scraping operation
type ScrapeProgress struct {
	InstanceType string             `json:"instance_type"`
	InstanceName string             `json:"instance_name"`
	Status       ScrapeStatus       `json:"status"`
	StartedAt    time.Time          `json:"started_at"`
	Progress     ScrapeProgressInfo `json:"progress"`
	Cancel       context.CancelFunc `json:"-"` // Not serialized
}

// ScrapeResult contains the final result of a scraping operation
type ScrapeResult struct {
	InstanceType   string       `json:"instance_type"`
	InstanceName   string       `json:"instance_name"`
	CompletedAt    time.Time    `json:"completed_at"`
	Status         ScrapeStatus `json:"status"`
	ProcessedCount int          `json:"processed_count"`
	ErrorCount     int          `json:"error_count"`
	ErrorMessage   *string      `json:"error_message,omitempty"`
}

// ScraperInstanceState holds runtime state for a scraper instance
type ScraperInstanceState struct {
	LastScrapeAt     *time.Time      `json:"last_scrape_at"`
	LastScrapeResult *ScrapeResult   `json:"last_result"`
	ActiveStatus     *ScrapeProgress `json:"active_status"`
}

// ConfigInstance represents a scraper instance from configuration
type ConfigInstance struct {
	Name                string                      `json:"name"`
	Type                string                      `json:"type"` // "radarr" or "sonarr"
	URL                 string                      `json:"url"`
	APIKey              string                      `json:"api_key"`
	Enabled             bool                        `json:"enabled"`
	ScrapeIntervalHours int                         `json:"scrape_interval_hours"`
	PathMappings        []config.PathMappingConfig  `json:"path_mappings"`
}

// Service manages the scraping of Radarr and Sonarr instances using configuration-first approach
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
	instanceState map[string]*ScraperInstanceState // key: "type:name"
}

// NewService creates a new scraper service with configuration-first approach
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
		instanceState: make(map[string]*ScraperInstanceState),
	}
}

// getInstanceKey generates a unique key for an instance
func getInstanceKey(instanceType, instanceName string) string {
	return instanceType + ":" + instanceName
}

// getConfigInstances returns all scraper instances from current configuration
func (s *Service) getConfigInstances() []*ConfigInstance {
	cfg := s.configGetter()
	instances := make([]*ConfigInstance, 0)

	// Convert Radarr instances
	if len(cfg.Scraper.RadarrInstances) > 0 {
		for _, radarrConfig := range cfg.Scraper.RadarrInstances {
			scrapeInterval := 24 // default
			if radarrConfig.ScrapeIntervalHours != nil {
				scrapeInterval = *radarrConfig.ScrapeIntervalHours
			}

			instance := &ConfigInstance{
				Name:                radarrConfig.Name,
				Type:                "radarr",
				URL:                 radarrConfig.URL,
				APIKey:              radarrConfig.APIKey,
				Enabled:             radarrConfig.Enabled != nil && *radarrConfig.Enabled,
				ScrapeIntervalHours: scrapeInterval,
				PathMappings:        radarrConfig.PathMappings,
			}
			instances = append(instances, instance)
		}
	}

	// Convert Sonarr instances
	if len(cfg.Scraper.SonarrInstances) > 0 {
		for _, sonarrConfig := range cfg.Scraper.SonarrInstances {
			scrapeInterval := 24 // default
			if sonarrConfig.ScrapeIntervalHours != nil {
				scrapeInterval = *sonarrConfig.ScrapeIntervalHours
			}

			instance := &ConfigInstance{
				Name:                sonarrConfig.Name,
				Type:                "sonarr",
				URL:                 sonarrConfig.URL,
				APIKey:              sonarrConfig.APIKey,
				Enabled:             sonarrConfig.Enabled != nil && *sonarrConfig.Enabled,
				ScrapeIntervalHours: scrapeInterval,
				PathMappings:        sonarrConfig.PathMappings,
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
func (s *Service) getOrCreateInstanceState(instanceType, instanceName string) *ScraperInstanceState {
	s.stateMutex.Lock()
	defer s.stateMutex.Unlock()

	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists {
		state = &ScraperInstanceState{}
		s.instanceState[key] = state
	}

	return state
}

// Start starts the scraper service
func (s *Service) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("scraper service is already running")
	}

	s.running = true
	s.wg.Add(1)
	go s.scraperLoop()

	s.logger.Info("Scraper service started")
	return nil
}

// Stop stops the scraper service
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

	s.logger.Info("Scraper service stopped")
	return nil
}

// scraperLoop runs the main scraping loop
func (s *Service) scraperLoop() {
	defer s.wg.Done()

	// For now, we only support manual scraping
	// Scheduled scraping can be added later if needed

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(30 * time.Second):
			// Check if we should do any scheduled work
			s.performScrape()
		}
	}
}

// performScrape checks for instances that need scraping and scrapes them
func (s *Service) performScrape() {
	// For now, disable automatic scraping - only support manual scraping
	s.logger.Debug("Scheduled scraping temporarily disabled - using manual scraping only")
	return
}

// TriggerScrape triggers a manual scrape for a specific instance by type and name
func (s *Service) TriggerScrape(instanceType, instanceName string) error {
	// Find the instance in configuration
	instance, err := s.findConfigInstance(instanceType, instanceName)
	if err != nil {
		return fmt.Errorf("instance not found in configuration: %w", err)
	}

	if !instance.Enabled {
		return fmt.Errorf("instance is disabled: %s/%s", instanceType, instanceName)
	}

	// Start scraping in a goroutine
	go s.scrapeConfigInstance(instance)

	return nil
}

// scrapeConfigInstance performs scraping for a configuration-based instance
func (s *Service) scrapeConfigInstance(instance *ConfigInstance) {
	s.logger.Info("Starting manual scrape", "instance", instance.Name, "type", instance.Type)

	// Get or create state for this instance
	state := s.getOrCreateInstanceState(instance.Type, instance.Name)

	// Check if already scraping this instance (only block if actively running)
	s.stateMutex.RLock()
	if state.ActiveStatus != nil && (state.ActiveStatus.Status == ScrapeStatusRunning || state.ActiveStatus.Status == ScrapeStatusCancelling) {
		s.stateMutex.RUnlock()
		s.logger.Warn("Instance is already being scraped", "instance", instance.Name, "type", instance.Type)
		return
	}
	s.stateMutex.RUnlock()

	// Create cancellation context for this scrape
	_, cancel := context.WithCancel(s.ctx)
	defer cancel()

	// Initialize progress tracking
	progress := &ScrapeProgress{
		InstanceType: instance.Type,
		InstanceName: instance.Name,
		Status:       ScrapeStatusRunning,
		StartedAt:    time.Now(),
		Progress: ScrapeProgressInfo{
			ProcessedCount: 0,
			ErrorCount:     0,
			CurrentBatch:   "initializing",
		},
		Cancel: cancel,
	}

	// Register active scraping
	s.stateMutex.Lock()
	state.ActiveStatus = progress
	s.stateMutex.Unlock()

	// Ensure cleanup on exit - preserve final status for visibility
	defer func() {
		if progress.Status == ScrapeStatusRunning || progress.Status == ScrapeStatusCancelling {
			// Only clear status if we're still in a transient state
			s.stateMutex.Lock()
			state.ActiveStatus = nil
			s.stateMutex.Unlock()
		}
		// For completed/failed states, keep ActiveStatus so frontend can see the final result
	}()

	var err error
	if instance.Type == "radarr" {
		err = s.scrapeRadarrConfig(instance, progress)
	} else if instance.Type == "sonarr" {
		err = s.scrapeSonarrConfig(instance, progress)
	} else {
		err = fmt.Errorf("unsupported instance type: %s", instance.Type)
	}

	// Update final status
	if err != nil {
		progress.Status = ScrapeStatusFailed
		s.logger.Error("Scraping failed", "instance", instance.Name, "type", instance.Type, "error", err)
	} else {
		progress.Status = ScrapeStatusCompleted
		s.logger.Info("Scraping completed", "instance", instance.Name, "type", instance.Type)
	}

	// Record the final result
	result := &ScrapeResult{
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
	state.LastScrapeAt = &result.CompletedAt
	state.LastScrapeResult = result
	s.stateMutex.Unlock()

	duration := time.Since(progress.StartedAt)
	s.logger.Info("Scrape completed",
		"instance", instance.Name,
		"type", instance.Type,
		"duration", duration)
}

// scrapeRadarrConfig scrapes a Radarr instance using configuration
func (s *Service) scrapeRadarrConfig(instance *ConfigInstance, progress *ScrapeProgress) error {
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

			mediaFile := database.MediaFileInput{
				InstanceName: instance.Name,
				InstanceType: "radarr",
				ExternalID:   int64(movie.ID),
				FilePath:     s.applyPathMappings(movie.MovieFile.Path, instance),
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

// scrapeSonarrConfig scrapes a Sonarr instance using configuration
func (s *Service) scrapeSonarrConfig(instance *ConfigInstance, progress *ScrapeProgress) error {
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

			mediaFile := database.MediaFileInput{
				InstanceName: instance.Name,
				InstanceType: "sonarr",
				ExternalID:   int64(episodeFile.ID),
				FilePath:     s.applyPathMappings(episodeFile.Path, instance),
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

// applyPathMappings transforms a file path using configured path mappings
// It finds the longest matching prefix and replaces it with the mapped path
func (s *Service) applyPathMappings(originalPath string, instance *ConfigInstance) string {
	if len(instance.PathMappings) == 0 {
		return originalPath
	}
	
	// Find the longest matching prefix
	var bestMatch *config.PathMappingConfig
	longestMatch := 0
	
	for i := range instance.PathMappings {
		mapping := &instance.PathMappings[i]
		if strings.HasPrefix(originalPath, mapping.FromPath) && len(mapping.FromPath) > longestMatch {
			bestMatch = mapping
			longestMatch = len(mapping.FromPath)
		}
	}
	
	if bestMatch != nil && longestMatch > 0 {
		mappedPath := strings.Replace(originalPath, bestMatch.FromPath, bestMatch.ToPath, 1)
		s.logger.Debug("Applied path mapping",
			"original", originalPath,
			"mapped", mappedPath,
			"from", bestMatch.FromPath,
			"to", bestMatch.ToPath)
		return mappedPath
	}
	
	return originalPath
}

// GetScrapeStatus returns the current scrape status for an instance by type and name
func (s *Service) GetScrapeStatus(instanceType, instanceName string) (*ScrapeProgress, error) {
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

// GetLastScrapeResult returns the last scrape result for an instance by type and name
func (s *Service) GetLastScrapeResult(instanceType, instanceName string) (*ScrapeResult, error) {
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

	return state.LastScrapeResult, nil
}

// GetAllActiveScrapes returns all currently active scrapes
func (s *Service) GetAllActiveScrapes() []*ScrapeProgress {
	s.stateMutex.RLock()
	defer s.stateMutex.RUnlock()

	var activeScrapes []*ScrapeProgress
	for _, state := range s.instanceState {
		if state.ActiveStatus != nil {
			activeScrapes = append(activeScrapes, state.ActiveStatus)
		}
	}

	return activeScrapes
}

// GetAllInstances returns all scraper instances from configuration with their state
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

// CancelScrape cancels an active scrape by type and name
func (s *Service) CancelScrape(instanceType, instanceName string) error {
	s.stateMutex.RLock()
	key := getInstanceKey(instanceType, instanceName)
	state, exists := s.instanceState[key]
	if !exists || state.ActiveStatus == nil {
		s.stateMutex.RUnlock()
		return fmt.Errorf("no active scrape found for instance: %s/%s", instanceType, instanceName)
	}

	progress := state.ActiveStatus
	s.stateMutex.RUnlock()

	// Cancel the operation
	if progress.Cancel != nil {
		progress.Cancel()
		progress.Status = ScrapeStatusCancelling
		s.logger.Info("Cancelled scrape", "instance", instanceName, "type", instanceType)
		return nil
	}

	return fmt.Errorf("cannot cancel scrape - no cancellation context available")
}

// TestConnection tests the connection to a scraper instance
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
