package api

import (
	"log/slog"

	"github.com/gofiber/fiber/v2"
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

// ArrsWebhookRequest represents a webhook payload from Radarr/Sonarr
type ArrsWebhookRequest struct {
	EventType string `json:"eventType"`
	FilePath  string `json:"filePath,omitempty"`
	// For upgrades/renames, the file path might be in other fields or need to be inferred
	Movie struct {
		FolderPath string `json:"folderPath"`
	} `json:"movie,omitempty"`
	Series struct {
		Path string `json:"path"`
	} `json:"series,omitempty"`
	EpisodeFile struct {
		Path string `json:"path"`
	} `json:"episodeFile,omitempty"`
	UpgradeTopics []struct {
		EntryId     int    `json:"entryId"`
		EpisodeId   int    `json:"episodeId"`
		LanguageId  int    `json:"languageId"`
		QualityName string `json:"qualityName"`
	} `json:"upgradeTopics,omitempty"`
	DeletedFiles []struct {
		Path string `json:"path"`
	} `json:"deletedFiles,omitempty"`
}

// handleArrsWebhook handles webhooks from Radarr/Sonarr
func (s *Server) handleArrsWebhook(c *fiber.Ctx) error {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "Arrs service is not available for webhook")
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	var req ArrsWebhookRequest
	if err := c.BodyParser(&req); err != nil {
		slog.ErrorContext(c.Context(), "Failed to parse webhook body", "error", err)
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
		})
	}

	slog.InfoContext(c.Context(), "Received ARR webhook", "event_type", req.EventType)

	// Determine file path to scan based on event type
	var pathsToScan []string

	switch req.EventType {
	case "Download": // OnImport (renamed from Download in v3/v4 but webhooks might use either)
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}
	case "Rename":
		// For rename, we want to scan the new file
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}
		// Also scan the series/movie folder to pick up changes
		if req.Series.Path != "" {
			pathsToScan = append(pathsToScan, req.Series.Path)
		} else if req.Movie.FolderPath != "" {
			pathsToScan = append(pathsToScan, req.Movie.FolderPath)
		}
	case "Upgrade":
		// For upgrade, scan the new file
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}
		
		// If we have deleted files information (sometimes sent in payload), scan those too to remove them
		for _, deleted := range req.DeletedFiles {
			if deleted.Path != "" {
				pathsToScan = append(pathsToScan, deleted.Path)
			}
		}
	default:
		slog.DebugContext(c.Context(), "Ignoring unhandled webhook event", "event_type", req.EventType)
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Ignored"})
	}

	if len(pathsToScan) == 0 {
		slog.WarnContext(c.Context(), "No file path found in webhook payload to scan")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "No path to scan"})
	}

	// Trigger scan for each path
	// We use TriggerScanForFile which launches a background task
	for _, path := range pathsToScan {
		// Since we don't know exactly which instance sent this, we might want to broadcast
		// or rely on path matching. TriggerScanForFile attempts path matching.
		// However, for webhooks, we might want a specific method that refreshes 
		// metadata for that path directly in AltMount if it's a file deletion/addition.
		// BUT: AltMount's main job is serving files. If a file is added/removed by ARR,
		// AltMount needs to update its internal metadata/VFS.
		
		// Ideally, we would tell the MetadataService or ImportService to re-scan this path.
		// But TriggerScanForFile in arrsService currently tells the ARR to rescan (circular?).
		// Wait, TriggerScanForFile sends "RefreshMonitoredDownloads" command to ARR. 
		// That's for when we import a file and want ARR to notice it.
		
		// Here, ARR is telling US it changed a file. We need to update OUR view.
		// We should call the library sync worker or metadata service directly.
		
		slog.InfoContext(c.Context(), "Processing webhook file update", "path", path)
		
		// If it's a file path, check/update metadata
		// We can use the health worker to "check" this file, which will fix metadata if needed?
		// Or better: Use the library sync worker to sync just this file/folder?
		// LibrarySyncWorker doesn't expose single-file sync publically yet, 
		// but we can add a health check which will verify the file existence/metadata.
		
		// Add to health check queue with high priority
		// This will verify if the file exists, read its metadata, update DB.
		// If it was deleted, health check might fail or mark as corrupted/missing.
		
		// Actually, if it's an Upgrade, the old file is gone and new one is there.
		// We should probably trigger a library sync for efficiency if many files change,
		// but for single file, a health check is a good start.
		
		// Use health repo to add check
		// We need to resolve the s struct to access healthRepo which isn't directly on Server struct here
		// But Server has healthRepo.
		// Wait, this function is on Server (*Server).
		
		if s.healthRepo != nil {
			// Add to health check (pending status)
			err := s.healthRepo.AddFileToHealthCheck(c.Context(), path, 1, nil)
			if err != nil {
				slog.ErrorContext(c.Context(), "Failed to add webhook file to health check", "path", path, "error", err)
			} else {
				slog.InfoContext(c.Context(), "Added file to health check queue from webhook", "path", path)
			}
		}
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Webhook processed",
	})
}

// ArrsInstanceResponse represents an arrs instance in API responses
type ArrsInstanceResponse struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	URL     string `json:"url"`
	Enabled bool   `json:"enabled"`
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

// handleListArrsInstances returns all arrs instances
func (s *Server) handleListArrsInstances(c *fiber.Ctx) error {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "Arrs service is not available")
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	slog.DebugContext(c.Context(), "Listing arrs instances")
	instances := s.arrsService.GetAllInstances()
	slog.DebugContext(c.Context(), "Found arrs instances", "count", len(instances))

	response := make([]*ArrsInstanceResponse, len(instances))
	for i, instance := range instances {
		response[i] = &ArrsInstanceResponse{
			Name:    instance.Name,
			Type:    instance.Type,
			URL:     instance.URL,
			Enabled: instance.Enabled,
		}
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetArrsInstance returns a single arrs instance by type and name
func (s *Server) handleGetArrsInstance(c *fiber.Ctx) error {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "Arrs service is not available")
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	instanceType := c.Params("type")
	instanceName := c.Params("name")

	if instanceType == "" || instanceName == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Instance type and name are required",
		})
	}

	slog.DebugContext(c.Context(), "Getting arrs instance", "type", instanceType, "name", instanceName)
	instance := s.arrsService.GetInstance(instanceType, instanceName)
	if instance == nil {
		slog.DebugContext(c.Context(), "Arrs instance not found", "type", instanceType, "name", instanceName)
		return c.Status(404).JSON(fiber.Map{
			"success": false,
			"message": "Instance not found",
		})
	}

	response := &ArrsInstanceResponse{
		Name:    instance.Name,
		Type:    instance.Type,
		URL:     instance.URL,
		Enabled: instance.Enabled,
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleTestArrsConnection tests connection to an arrs instance
func (s *Server) handleTestArrsConnection(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	var req TestConnectionRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "Invalid request body",
			"details": err.Error(),
		})
	}

	if req.URL == "" || req.APIKey == "" {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"message": "URL and API key are required",
		})
	}

	if err := s.arrsService.TestConnection(c.Context(), string(req.Type), req.URL, req.APIKey); err != nil {
		return c.Status(400).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Connection successful",
	})
}

// handleGetArrsStats returns arrs statistics
func (s *Server) handleGetArrsStats(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	// Get all instances from configuration
	instances := s.arrsService.GetAllInstances()

	// Calculate stats from instances
	var totalRadarr, enabledRadarr, totalSonarr, enabledSonarr int
	for _, instance := range instances {
		switch instance.Type {
		case "radarr":
			totalRadarr++
			if instance.Enabled {
				enabledRadarr++
			}
		case "sonarr":
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

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetArrsHealth returns health checks from all ARR instances
func (s *Server) handleGetArrsHealth(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	health, err := s.arrsService.GetHealth(c.Context())
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to get ARR health",
			"error":   err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    health,
	})
}
