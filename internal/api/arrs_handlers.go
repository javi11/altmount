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
