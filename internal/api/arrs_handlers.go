package api

import (
	"encoding/json"
	"net/http"
)

// ArrsInstanceRequest represents a request to create/update an arrs instance
type ArrsInstanceRequest struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	URL               string `json:"url"`
	APIKey            string `json:"api_key"`
	Enabled           bool   `json:"enabled"`
	SyncIntervalHours int    `json:"sync_interval_hours"`
}

// ArrsInstanceResponse represents an arrs instance in API responses
type ArrsInstanceResponse struct {
	ID                int64   `json:"id"`
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	URL               string  `json:"url"`
	Enabled           bool    `json:"enabled"`
	SyncIntervalHours int     `json:"sync_interval_hours"`
	LastSyncAt        *string `json:"last_sync_at"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
}

// ArrsStatsResponse represents arrs statistics
type ArrsStatsResponse struct {
	TotalInstances   int     `json:"total_instances"`
	EnabledInstances int     `json:"enabled_instances"`
	TotalRadarr      int     `json:"total_radarr"`
	EnabledRadarr    int     `json:"enabled_radarr"`
	TotalSonarr      int     `json:"total_sonarr"`
	EnabledSonarr    int     `json:"enabled_sonarr"`
	DueForSync       int     `json:"due_for_sync"`
	LastSync         *string `json:"last_sync"`
}

// ArrsMovieResponse represents a movie in API responses
type ArrsMovieResponse struct {
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

// ArrsEpisodeResponse represents an episode in API responses
type ArrsEpisodeResponse struct {
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

// SyncProgressResponse represents sync progress in API responses
type SyncProgressResponse struct {
	InstanceID     int64  `json:"instance_id"`
	Status         string `json:"status"`
	StartedAt      string `json:"started_at"`
	ProcessedCount int    `json:"processed_count"`
	ErrorCount     int    `json:"error_count"`
	TotalItems     *int   `json:"total_items,omitempty"`
	CurrentBatch   string `json:"current_batch"`
}

// SyncResultResponse represents sync result in API responses
type SyncResultResponse struct {
	InstanceID     int64   `json:"instance_id"`
	Status         string  `json:"status"`
	StartedAt      string  `json:"started_at"`
	CompletedAt    string  `json:"completed_at"`
	ProcessedCount int     `json:"processed_count"`
	ErrorCount     int     `json:"error_count"`
	ErrorMessage   *string `json:"error_message,omitempty"`
}

// handleListArrsInstances returns all arrs instances
func (s *Server) handleListArrsInstances(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		s.logger.Error("Arrs service is not available")
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	s.logger.Debug("Listing arrs instances")
	instances := s.arrsService.GetAllInstances()
	s.logger.Debug("Found arrs instances", "count", len(instances))

	response := make([]*ArrsInstanceResponse, len(instances))
	for i, instance := range instances {
		response[i] = &ArrsInstanceResponse{
			ID:                0, // No longer using database IDs
			Name:              instance.Name,
			Type:              instance.Type,
			URL:               instance.URL,
			Enabled:           instance.Enabled,
			SyncIntervalHours: instance.SyncIntervalHours,
			CreatedAt:         "", // No longer tracked
			UpdatedAt:         "", // No longer tracked
		}

		// Get state information from in-memory state
		if state, err := s.arrsService.GetLastSyncResult(instance.Type, instance.Name); err == nil && state != nil {
			lastSync := state.CompletedAt.Format("2006-01-02T15:04:05Z")
			response[i].LastSyncAt = &lastSync
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetArrsInstance returns a single arrs instance by type and name
func (s *Server) handleGetArrsInstance(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		s.logger.Error("Arrs service is not available")
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting arrs instance", "type", instanceType, "name", instanceName)
	instance := s.arrsService.GetInstance(instanceType, instanceName)
	if instance == nil {
		s.logger.Debug("Arrs instance not found", "type", instanceType, "name", instanceName)
		http.Error(w, "Instance not found", http.StatusNotFound)
		return
	}

	response := &ArrsInstanceResponse{
		ID:                0, // No longer using database IDs
		Name:              instance.Name,
		Type:              instance.Type,
		URL:               instance.URL,
		Enabled:           instance.Enabled,
		SyncIntervalHours: instance.SyncIntervalHours,
		CreatedAt:         "", // No longer tracked
		UpdatedAt:         "", // No longer tracked
	}

	// Get state information from in-memory state
	if state, err := s.arrsService.GetLastSyncResult(instanceType, instanceName); err == nil && state != nil {
		lastSync := state.CompletedAt.Format("2006-01-02T15:04:05Z")
		response.LastSyncAt = &lastSync
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleCreateArrsInstance creates a new arrs instance (now deprecated - use config instead)
func (s *Server) handleCreateArrsInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Creating instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleUpdateArrsInstance updates an existing arrs instance (now deprecated - use config instead)
func (s *Server) handleUpdateArrsInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Updating instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleDeleteArrsInstance deletes an arrs instance (now deprecated - use config instead)
func (s *Server) handleDeleteArrsInstance(w http.ResponseWriter, r *http.Request) {
	// This endpoint is deprecated in favor of configuration-first approach
	http.Error(w, "Deleting instances via API is no longer supported. Please use configuration file.", http.StatusMethodNotAllowed)
}

// handleTestArrsConnection tests connection to an arrs instance
func (s *Server) handleTestArrsConnection(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
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

	if err := s.arrsService.TestConnection(string(req.Type), req.URL, req.APIKey); err != nil {
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

// handleTriggerSync manually triggers a sync for an instance
func (s *Server) handleTriggerSync(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	if err := s.arrsService.TriggerSync(instanceType, instanceName); err != nil {
		s.logger.Error("Failed to trigger sync",
			"type", instanceType,
			"name", instanceName,
			"error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Sync triggered successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetArrsStats returns arrs statistics
func (s *Server) handleGetArrsStats(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	// Get all instances from configuration
	instances := s.arrsService.GetAllInstances()

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

	response := &ArrsStatsResponse{
		TotalInstances:   totalRadarr + totalSonarr,
		EnabledInstances: enabledRadarr + enabledSonarr,
		TotalRadarr:      totalRadarr,
		EnabledRadarr:    enabledRadarr,
		TotalSonarr:      totalSonarr,
		EnabledSonarr:    enabledSonarr,
		DueForSync:       0, // Not applicable with config-first approach
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

// handleGetSyncStatus returns the current sync status for an instance
func (s *Server) handleGetSyncStatus(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		s.logger.Error("Arrs service is not available")
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting sync status", "type", instanceType, "name", instanceName)

	progress, err := s.arrsService.GetSyncStatus(instanceType, instanceName)
	if err != nil {
		s.logger.Debug("No sync status found", "type", instanceType, "name", instanceName, "error", err)
		http.Error(w, "No active sync for this instance", http.StatusNotFound)
		return
	}

	// Check if progress is nil (no active scraping)
	if progress == nil {
		s.logger.Debug("No active sync status", "type", instanceType, "name", instanceName)
		http.Error(w, "No active sync for this instance", http.StatusNotFound)
		return
	}

	response := &SyncProgressResponse{
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

// handleGetLastSyncResult returns the last sync result for an instance
func (s *Server) handleGetLastSyncResult(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		s.logger.Error("Arrs service is not available")
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	s.logger.Debug("Getting last sync result", "type", instanceType, "name", instanceName)

	result, err := s.arrsService.GetLastSyncResult(instanceType, instanceName)
	if err != nil {
		s.logger.Debug("No sync result found", "type", instanceType, "name", instanceName, "error", err)
		http.Error(w, "No sync result found for this instance", http.StatusNotFound)
		return
	}

	// Check if result is nil (no result available)
	if result == nil {
		s.logger.Debug("No sync result available", "type", instanceType, "name", instanceName)
		http.Error(w, "No sync result found for this instance", http.StatusNotFound)
		return
	}

	completedAtStr := result.CompletedAt.Format("2006-01-02T15:04:05Z")

	response := &SyncResultResponse{
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

// handleCancelSync cancels an active sync operation
func (s *Server) handleCancelSync(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	instanceType := r.PathValue("type")
	instanceName := r.PathValue("name")

	if instanceType == "" || instanceName == "" {
		http.Error(w, "Instance type and name are required", http.StatusBadRequest)
		return
	}

	if err := s.arrsService.CancelSync(instanceType, instanceName); err != nil {
		s.logger.Error("Failed to cancel sync",
			"type", instanceType,
			"name", instanceName,
			"error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	response := map[string]interface{}{
		"success": true,
		"message": "Sync cancelled successfully",
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// handleGetAllActiveSyncs returns all currently active sync operations
func (s *Server) handleGetAllActiveSyncs(w http.ResponseWriter, r *http.Request) {
	if s.arrsService == nil {
		http.Error(w, "Arrs not available", http.StatusServiceUnavailable)
		return
	}

	activeSyncs := s.arrsService.GetAllActiveSyncs()

	response := make([]*SyncProgressResponse, 0, len(activeSyncs))
	for _, progress := range activeSyncs {
		response = append(response, &SyncProgressResponse{
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
