package api

import (
	"encoding/json"
	"net/http"
)

// ScraperInstanceRequest represents a request to create/update a scraper instance
type ScraperInstanceRequest struct {
	Name                string `json:"name"`
	Type                string `json:"type"`
	URL                 string `json:"url"`
	APIKey              string `json:"api_key"`
	Enabled             bool   `json:"enabled"`
	ScrapeIntervalHours int    `json:"scrape_interval_hours"`
}

// ScraperInstanceResponse represents a scraper instance in API responses
type ScraperInstanceResponse struct {
	ID                  int64   `json:"id"`
	Name                string  `json:"name"`
	Type                string  `json:"type"`
	URL                 string  `json:"url"`
	Enabled             bool    `json:"enabled"`
	ScrapeIntervalHours int     `json:"scrape_interval_hours"`
	LastScrapeAt        *string `json:"last_scrape_at"`
	CreatedAt           string  `json:"created_at"`
	UpdatedAt           string  `json:"updated_at"`
}

// ScraperStatsResponse represents scraper statistics
type ScraperStatsResponse struct {
	TotalInstances   int     `json:"total_instances"`
	EnabledInstances int     `json:"enabled_instances"`
	TotalRadarr      int     `json:"total_radarr"`
	EnabledRadarr    int     `json:"enabled_radarr"`
	TotalSonarr      int     `json:"total_sonarr"`
	EnabledSonarr    int     `json:"enabled_sonarr"`
	DueForScrape     int     `json:"due_for_scrape"`
	LastScrape       *string `json:"last_scrape"`
}

// ScraperMovieResponse represents a movie in API responses
type ScraperMovieResponse struct {
	ID          int64   `json:"id"`
	InstanceID  int64   `json:"instance_id"`
	MovieID     int64   `json:"movie_id"`
	Title       string  `json:"title"`
	Year        *int    `json:"year"`
	FilePath    string  `json:"file_path"`
	FileSize    *int64  `json:"file_size"`
	Quality     *string `json:"quality"`
	IMDbID      *string `json:"imdb_id"`
	TMDbID      *int64  `json:"tmdb_id"`
	LastUpdated string  `json:"last_updated"`
}

// ScraperEpisodeResponse represents an episode in API responses
type ScraperEpisodeResponse struct {
	ID            int64   `json:"id"`
	InstanceID    int64   `json:"instance_id"`
	SeriesID      int64   `json:"series_id"`
	EpisodeID     int64   `json:"episode_id"`
	SeriesTitle   string  `json:"series_title"`
	SeasonNumber  int     `json:"season_number"`
	EpisodeNumber int     `json:"episode_number"`
	EpisodeTitle  *string `json:"episode_title"`
	FilePath      string  `json:"file_path"`
	FileSize      *int64  `json:"file_size"`
	Quality       *string `json:"quality"`
	AirDate       *string `json:"air_date"`
	TVDbID        *int64  `json:"tvdb_id"`
	IMDbID        *string `json:"imdb_id"`
	LastUpdated   string  `json:"last_updated"`
}

// TestConnectionRequest represents a request to test connection
type TestConnectionRequest struct {
	Type   string `json:"type"`
	URL    string `json:"url"`
	APIKey string `json:"api_key"`
}

// ScrapeProgressResponse represents scrape progress in API responses
type ScrapeProgressResponse struct {
	InstanceID     int64  `json:"instance_id"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	ProcessedCount int    `json:"processed_count"`
	ErrorCount     int    `json:"error_count"`
	TotalItems     *int   `json:"total_items,omitempty"`
	CurrentBatch   string `json:"current_batch"`
}

// ScrapeResultResponse represents scrape result in API responses
type ScrapeResultResponse struct {
	InstanceID     int64   `json:"instance_id"`
	Status         string  `json:"status"`
	StartedAt      string  `json:"started_at"`
	CompletedAt    string  `json:"completed_at"`
	ProcessedCount int     `json:"processed_count"`
	ErrorCount     int     `json:"error_count"`
	ErrorMessage   *string `json:"error_message,omitempty"`
}

// handleListScraperInstances returns all scraper instances
func (s *Server) handleListScraperInstances(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		s.logger.Error("Scraper service is not available")
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	s.logger.Debug("Listing scraper instances")
	instances := s.scraperService.GetAllInstances()
	s.logger.Debug("Found scraper instances", "count", len(instances))

	response := make([]*ScraperInstanceResponse, len(instances))
	for i, instance := range instances {
		response[i] = &ScraperInstanceResponse{
			ID:                  0, // No longer using database IDs
			Name:                instance.Name,
			Type:                instance.Type,
			URL:                 instance.URL,
			Enabled:             instance.Enabled,
			ScrapeIntervalHours: instance.ScrapeIntervalHours,
			CreatedAt:           "", // No longer tracked
			UpdatedAt:           "", // No longer tracked
		}

		// Get state information from in-memory state
		if state, err := s.scraperService.GetLastScrapeResult(instance.Type, instance.Name); err == nil && state != nil {
			lastScrape := state.CompletedAt.Format("2006-01-02T15:04:05Z")
			response[i].LastScrapeAt = &lastScrape
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetScraperInstance returns a single scraper instance by type and name
func (s *Server) handleGetScraperInstance(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		s.logger.Error("Scraper service is not available")
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting scraper instance", "type", instanceType, "name", instanceName)
	instance := s.scraperService.GetInstance(instanceType, instanceName)
	if instance == nil {
		s.logger.Debug("Scraper instance not found", "type", instanceType, "name", instanceName)
		http.Error(w, "Instance not found", http.StatusNotFound)
		return
	}

	response := &ScraperInstanceResponse{
		ID:                  0, // No longer using database IDs
		Name:                instance.Name,
		Type:                instance.Type,
		URL:                 instance.URL,
		Enabled:             instance.Enabled,
		ScrapeIntervalHours: instance.ScrapeIntervalHours,
		CreatedAt:           "", // No longer tracked
		UpdatedAt:           "", // No longer tracked
	}

	// Get state information from in-memory state
	if state, err := s.scraperService.GetLastScrapeResult(instanceType, instanceName); err == nil && state != nil {
		lastScrape := state.CompletedAt.Format("2006-01-02T15:04:05Z")
		response.LastScrapeAt = &lastScrape
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateScraperInstance creates a new scraper instance (now deprecated - use config instead)
func (s *Server) handleCreateScraperInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Creating instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleUpdateScraperInstance updates an existing scraper instance (now deprecated - use config instead)
func (s *Server) handleUpdateScraperInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Updating instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleDeleteScraperInstance deletes a scraper instance (now deprecated - use config instead)
func (s *Server) handleDeleteScraperInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Deleting instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleTestScraperConnection tests connection to a scraper instance
func (s *Server) handleTestScraperConnection(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	var req TestConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" || req.APIKey == "" {
		http.Error(w, "URL and API key are required", http.StatusBadRequest)
		return
	}

	if err := s.scraperService.TestConnection(string(req.Type), req.URL, req.APIKey); err != nil {
		response := map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(response)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Connection successful",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleTriggerScrape manually triggers a scrape for an instance
func (s *Server) handleTriggerScrape(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	if err := s.scraperService.TriggerScrape(instanceType, instanceName); err != nil {
		s.logger.Error("Failed to trigger scrape",
			"type", instanceType,
			"name", instanceName,
			"error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Scrape triggered successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetScraperStats returns scraper statistics
func (s *Server) handleGetScraperStats(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	// Get all instances from configuration
	instances := s.scraperService.GetAllInstances()

	// Calculate stats from instances
	var totalRadarr, enabledRadarr, totalSonarr, enabledSonarr int
	for _, instance := range instances {
		if instance.Type == "radarr" {
			totalRadarr++
			if instance.Enabled {
				enabledRadarr++
			}
		} else if instance.Type == "sonarr" {
			totalSonarr++
			if instance.Enabled {
				enabledSonarr++
			}
		}
	}

	response := &ScraperStatsResponse{
		TotalInstances:   totalRadarr + totalSonarr,
		EnabledInstances: enabledRadarr + enabledSonarr,
		TotalRadarr:      totalRadarr,
		EnabledRadarr:    enabledRadarr,
		TotalSonarr:      totalSonarr,
		EnabledSonarr:    enabledSonarr,
		DueForScrape:     0, // Not applicable with config-first approach
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleSearchMovies searches for movies (deprecated - no longer stored in database)
func (s *Server) handleSearchMovies(w http.ResponseWriter, r *http.Request) {
	// Movies are no longer stored in database with configuration-first approach
	http.Error(w, "Movie search is no longer supported. Scraped data is not stored in database.", http.StatusMethodNotAllowed)
}

// handleSearchEpisodes searches for episodes (deprecated - no longer stored in database)
func (s *Server) handleSearchEpisodes(w http.ResponseWriter, r *http.Request) {
	// Episodes are no longer stored in database with configuration-first approach
	http.Error(w, "Episode search is no longer supported. Scraped data is not stored in database.", http.StatusMethodNotAllowed)
}

// handleGetScrapeStatus returns the current scrape status for an instance
func (s *Server) handleGetScrapeStatus(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		s.logger.Error("Scraper service is not available")
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting scrape status", "type", instanceType, "name", instanceName)

	progress, err := s.scraperService.GetScrapeStatus(instanceType, instanceName)
	if err != nil {
		s.logger.Debug("No scrape status found", "type", instanceType, "name", instanceName, "error", err)
		http.Error(w, "No active scraping for this instance", http.StatusNotFound)
		return
	}

	// Check if progress is nil (no active scraping)
	if progress == nil {
		s.logger.Debug("No active scraping status", "type", instanceType, "name", instanceName)
		http.Error(w, "No active scraping for this instance", http.StatusNotFound)
		return
	}

	response := &ScrapeProgressResponse{
		InstanceID:     0, // No longer used for config-based instances
		Status:         string(progress.Status),
		StartedAt:      progress.StartedAt.Format("2006-01-02T15:04:05Z"),
		ProcessedCount: progress.Progress.ProcessedCount,
		ErrorCount:     progress.Progress.ErrorCount,
		TotalItems:     progress.Progress.TotalItems,
		CurrentBatch:   progress.Progress.CurrentBatch,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetLastScrapeResult returns the last scrape result for an instance
func (s *Server) handleGetLastScrapeResult(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		s.logger.Error("Scraper service is not available")
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting last scrape result", "type", instanceType, "name", instanceName)

	result, err := s.scraperService.GetLastScrapeResult(instanceType, instanceName)
	if err != nil {
		s.logger.Debug("No scrape result found", "type", instanceType, "name", instanceName, "error", err)
		http.Error(w, "No scrape result found for this instance", http.StatusNotFound)
		return
	}

	// Check if result is nil (no result available)
	if result == nil {
		s.logger.Debug("No scrape result available", "type", instanceType, "name", instanceName)
		http.Error(w, "No scrape result found for this instance", http.StatusNotFound)
		return
	}

	completedAtStr := result.CompletedAt.Format("2006-01-02T15:04:05Z")

	response := &ScrapeResultResponse{
		InstanceID:     0, // No longer used for config-based instances
		Status:         string(result.Status),
		StartedAt:      completedAtStr, // Use CompletedAt as the time reference
		CompletedAt:    completedAtStr,
		ProcessedCount: result.ProcessedCount,
		ErrorCount:     result.ErrorCount,
		ErrorMessage:   result.ErrorMessage,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCancelScrape cancels an active scraping operation
func (s *Server) handleCancelScrape(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	if err := s.scraperService.CancelScrape(instanceType, instanceName); err != nil {
		s.logger.Error("Failed to cancel scrape",
			"type", instanceType,
			"name", instanceName,
			"error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Scrape cancelled successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetAllActiveScrapes returns all currently active scraping operations
func (s *Server) handleGetAllActiveScrapes(w http.ResponseWriter, r *http.Request) {
	if s.scraperService == nil {
		http.Error(w, "Scraper not available", http.StatusServiceUnavailable)
		return
	}

	activeScrapes := s.scraperService.GetAllActiveScrapes()

	response := make([]*ScrapeProgressResponse, 0, len(activeScrapes))
	for _, progress := range activeScrapes {
		response = append(response, &ScrapeProgressResponse{
			InstanceID:     0, // No longer used for config-based instances
			Status:         string(progress.Status),
			StartedAt:      progress.StartedAt.Format("2006-01-02T15:04:05Z"),
			ProcessedCount: progress.Progress.ProcessedCount,
			ErrorCount:     progress.Progress.ErrorCount,
			TotalItems:     progress.Progress.TotalItems,
			CurrentBatch:   progress.Progress.CurrentBatch,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
