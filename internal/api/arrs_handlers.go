package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/javi11/altmount/internal/database"
)

// ArrsInstanceRequest represents a request to create/update an arrs instance
type ArrsInstanceRequest struct {
	Name              string `json:"name"`
	Type              string `json:"type"`
	URL               string `json:"url"`
	APIKey            string `json:"api_key"`
	Category          string `json:"category"`
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
	MovieFile struct {
		Path string `json:"path"`
	} `json:"movieFile,omitempty"`
	Series struct {
		Path string `json:"path"`
	} `json:"series,omitempty"`
	EpisodeFile struct {
		Path string `json:"path"`
	} `json:"episodeFile,omitempty"`
	DeletedFiles ArrsDeletedFiles `json:"deletedFiles,omitempty"`
}

type ArrsDeletedFile struct {
	Path string `json:"path"`
}

type ArrsDeletedFiles []ArrsDeletedFile

func (df *ArrsDeletedFiles) UnmarshalJSON(data []byte) error {
	if string(data) == "false" || string(data) == "null" {
		*df = nil
		return nil
	}
	var a []ArrsDeletedFile
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*df = a
	return nil
}

// handleArrsWebhook handles webhooks from Radarr/Sonarr
func (s *Server) handleArrsWebhook(c *fiber.Ctx) error {
	// Check for API key authentication
	// Try query param first, then header
	apiKey := c.Query("apikey")
	if apiKey == "" {
		apiKey = c.Get("X-Api-Key")
	}

	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "API key required",
		})
	}

	// Validate API key
	if !s.validateAPIKey(c, apiKey) {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "Invalid API key",
		})
	}

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

	// Determine file path to scan/delete based on event type
	var pathsToScan []string
	var filesToDelete []string
	var dirsToDelete []string

	switch req.EventType {
	case "Test":
		slog.InfoContext(c.Context(), "Received ARR test webhook")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Test successful"})
	case "Download": // OnImport
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.MovieFile.Path != "" {
			pathsToScan = append(pathsToScan, req.MovieFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}
	case "Rename":
		// For rename, we want to scan the new file
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.MovieFile.Path != "" {
			pathsToScan = append(pathsToScan, req.MovieFile.Path)
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
		} else if req.MovieFile.Path != "" {
			pathsToScan = append(pathsToScan, req.MovieFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}

		// If we have deleted files information, mark for deletion
		for _, deleted := range req.DeletedFiles {
			if deleted.Path != "" {
				filesToDelete = append(filesToDelete, deleted.Path)
			}
		}
	case "MovieDelete", "SeriesDelete":
		slog.InfoContext(c.Context(), "Ignoring delete webhook to prevent accidental library wipes")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Ignored"})
	case "EpisodeFileDelete":
		slog.InfoContext(c.Context(), "Ignoring EpisodeFileDelete webhook to prevent accidental library wipes")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Ignored"})
	default:
		slog.DebugContext(c.Context(), "Ignoring unhandled webhook event", "event_type", req.EventType)
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Ignored"})
	}

	// Trigger scan for each path
	// We use TriggerScanForFile which launches a background task
	cfg := s.configManager.GetConfig()
	mountPath := cfg.MountPath
	importDir := ""
	if cfg.Import.ImportDir != nil {
		importDir = *cfg.Import.ImportDir
	}
	libraryDir := ""
	if cfg.Health.LibraryDir != nil {
		libraryDir = *cfg.Health.LibraryDir
	}

	// Helper for path normalization
	normalize := func(path string) string {
		normalizedPath := path

		// Find the longest matching prefix to avoid over-truncation
		prefixes := []string{}
		if mountPath != "" {
			prefixes = append(prefixes, mountPath)
		}
		if importDir != "" {
			prefixes = append(prefixes, importDir)
		}
		if libraryDir != "" {
			prefixes = append(prefixes, libraryDir)
		}

		longestPrefix := ""
		for _, p := range prefixes {
			if strings.HasPrefix(normalizedPath, p) && len(p) > len(longestPrefix) {
				longestPrefix = p
			}
		}

		if longestPrefix != "" {
			normalizedPath = strings.TrimPrefix(normalizedPath, longestPrefix)
		}
		normalizedPath = strings.TrimPrefix(normalizedPath, "/")

		// Special handling for STRM files
		if strings.HasSuffix(normalizedPath, ".strm") {
			// Resolve the real path from the .strm file content
			content, err := os.ReadFile(path)
			if err == nil {
				urlStr := strings.TrimSpace(string(content))
				if u, err := url.Parse(urlStr); err == nil {
					if p := u.Query().Get("path"); p != "" {
						normalizedPath = strings.TrimPrefix(p, "/")
					}
				}
			}
		}
		return normalizedPath
	}

	// Process File Deletions
	// SAFETY: Webhook should NEVER delete anything to prevent accidental library wipes (e.g. when mount is down)
	if false {
		for _, path := range filesToDelete {
			normalizedPath := normalize(path)

			// Safety check: Don't delete if we are about to scan this same path (e.g. in-place upgrade/rename)
			isBeingScanned := false
			for _, scanPath := range pathsToScan {
				if normalize(scanPath) == normalizedPath {
					isBeingScanned = true
					break
				}
			}

			if isBeingScanned {
				slog.InfoContext(c.Context(), "Skipping webhook file deletion because file is being upgraded/scanned",
					"path", normalizedPath,
					"event_type", req.EventType)
				continue
			}

			slog.InfoContext(c.Context(), "Processing webhook file deletion",
				"original_path", path,
				"normalized_path", normalizedPath,
				"event_type", req.EventType)

			// Delete health record
			if s.healthRepo != nil {
				if err := s.healthRepo.DeleteHealthRecord(c.Context(), normalizedPath); err != nil {
					slog.ErrorContext(c.Context(), "Failed to delete health record from webhook", "path", normalizedPath, "error", err)
				}
			}

			// Delete metadata
			/*
				if s.metadataService != nil {
					// Check if we should delete source NZB
					deleteSource := false
					if cfg.Metadata.DeleteSourceNzbOnRemoval != nil {
						deleteSource = *cfg.Metadata.DeleteSourceNzbOnRemoval
					}
					if err := s.metadataService.DeleteFileMetadataWithSourceNzb(c.Context(), normalizedPath, deleteSource); err != nil {
						// Log as debug because it might already be gone
						slog.DebugContext(c.Context(), "Failed to delete metadata from webhook (might be gone)", "path", normalizedPath, "error", err)
					}
				}
			*/
			slog.InfoContext(c.Context(), "Skipping metadata deletion (preserved by safety policy)", "path", normalizedPath)
		}
	}

	// Process Directory Deletions
	// SAFETY: Webhook should NEVER delete anything to prevent accidental library wipes (e.g. when mount is down)
	if false {
		for _, path := range dirsToDelete {
			normalizedPath := normalize(path)
			slog.InfoContext(c.Context(), "Processing webhook directory deletion",
				"original_path", path,
				"normalized_path", normalizedPath)

			// Delete health records for all files in this directory
			if s.healthRepo != nil {
				if count, err := s.healthRepo.DeleteHealthRecordsByPrefix(c.Context(), normalizedPath); err != nil {
					slog.ErrorContext(c.Context(), "Failed to delete health records by prefix from webhook", "prefix", normalizedPath, "error", err)
				} else {
					slog.InfoContext(c.Context(), "Deleted health records for directory", "prefix", normalizedPath, "count", count)
				}
			}

			// Delete metadata directory
			/*
				if s.metadataService != nil {
					if err := s.metadataService.DeleteDirectory(normalizedPath); err != nil {
						slog.DebugContext(c.Context(), "Failed to delete metadata directory from webhook (might be gone)", "path", normalizedPath, "error", err)
					}
				}
			*/
			slog.InfoContext(c.Context(), "Skipping metadata directory deletion (preserved by safety policy)", "path", normalizedPath)
		}
	}

	if len(pathsToScan) == 0 {
		slog.WarnContext(c.Context(), "No file path found in webhook payload to scan")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "No path to scan"})
	}

	for _, path := range pathsToScan {
		// Normalize path to relative
		normalizedPath := normalize(path)

		slog.InfoContext(c.Context(), "Processing webhook file update",
			"original_path", path,
			"normalized_path", normalizedPath)

		if s.healthRepo != nil {
			var releaseDate *time.Time
			var sourceNzb *string

			// Handle Rename and Download specifically: try to find and re-link old record
			if req.EventType == "Rename" || req.EventType == "Download" {
				fileName := filepath.Base(normalizedPath)
				// Try to find a record with the same filename but currently under /complete/
				// or with a NULL library_path
				if err := s.healthRepo.RelinkFileByFilename(c.Context(), fileName, normalizedPath, path); err == nil {
					slog.InfoContext(c.Context(), "Successfully re-linked health record during webhook",
						"event", req.EventType, "filename", fileName, "new_library_path", path)
					continue // Successfully re-linked, no need to add new
				}
			}

			// Try to read metadata to get release date
			if s.metadataService != nil {
				meta, err := s.metadataService.ReadFileMetadata(normalizedPath)
				if err == nil && meta != nil {
					if meta.ReleaseDate != 0 {
						t := time.Unix(meta.ReleaseDate, 0)
						releaseDate = &t
					}
					if meta.SourceNzbPath != "" {
						sourceNzb = &meta.SourceNzbPath
					}
				} else {
					// SAFETY: If metadata does not exist for this path, it means the file was renamed
					// and we don't have a record for the new name yet. We should NOT add a health
					// record for a path without metadata, as it will just be marked corrupted.
					// The Library Sync will eventually discover the new mapping.
					slog.DebugContext(c.Context(), "Skipping webhook health addition: no metadata found for path",
						"path", normalizedPath)
					continue
				}
			}

			// Add to health check (pending status) with high priority (Next) to ensure it's processed right away
			err := s.healthRepo.AddFileToHealthCheckWithMetadata(c.Context(), normalizedPath, 2, sourceNzb, database.HealthPriorityNext, releaseDate)
			if err != nil {
				slog.ErrorContext(c.Context(), "Failed to add webhook file to health check", "path", normalizedPath, "error", err)
			} else {
				slog.InfoContext(c.Context(), "Added file to health check queue from webhook with high priority", "path", normalizedPath)
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
	Name     string `json:"name"`
	Type     string `json:"type"`
	URL      string `json:"url"`
	Category string `json:"category"`
	Enabled  bool   `json:"enabled"`
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
			Name:     instance.Name,
			Type:     instance.Type,
			URL:      instance.URL,
			Category: instance.Category,
			Enabled:  instance.Enabled,
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
		Name:     instance.Name,
		Type:     instance.Type,
		URL:      instance.URL,
		Category: instance.Category,
		Enabled:  instance.Enabled,
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

// handleRegisterArrsWebhooks triggers automatic registration of webhooks in ARR instances
func (s *Server) handleRegisterArrsWebhooks(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "User not authenticated or no API key available",
		})
	}

	// Get configured base URL or use default
	baseURL := "http://altmount:8080"
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		if cfg.Arrs.WebhookBaseURL != "" {
			baseURL = cfg.Arrs.WebhookBaseURL
		}
	}

	// Launch in background to not block
	go func() {
		ctx := context.Background()
		if err := s.arrsService.EnsureWebhookRegistration(ctx, baseURL, apiKey); err != nil {
			slog.ErrorContext(ctx, "Failed to register webhooks", "error", err)
		}
	}()

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Webhook registration triggered in background",
	})
}

// handleRegisterArrsDownloadClients triggers automatic registration of AltMount as a download client in ARR instances
func (s *Server) handleRegisterArrsDownloadClients(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "User not authenticated or no API key available",
		})
	}

	// Get configured host/port or use default
	host := "altmount"
	port := 8080
	urlBase := "sabnzbd"
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		if cfg.SABnzbd.DownloadClientBaseURL != "" {
			rawURL := cfg.SABnzbd.DownloadClientBaseURL
			if !strings.Contains(rawURL, "://") {
				rawURL = "http://" + rawURL
			}
			if u, err := url.Parse(rawURL); err == nil {
				host = u.Hostname()
				if host == "" {
					host = "altmount"
				}
				if p := u.Port(); p != "" {
					if portVal, err := strconv.Atoi(p); err == nil {
						port = portVal
					}
				} else if u.Scheme == "https" {
					port = 443
				} else if u.Scheme == "http" {
					port = 80
				}
				if u.Path != "" && u.Path != "/" {
					urlBase = strings.Trim(u.Path, "/")
				}
			}
		} else {
			port = cfg.WebDAV.Port
		}
	}

	// Launch in background to not block
	go func() {
		ctx := context.Background()
		if err := s.arrsService.EnsureDownloadClientRegistration(ctx, host, port, urlBase, apiKey); err != nil {
			slog.ErrorContext(ctx, "Failed to register download clients", "error", err)
		}
	}()

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"message": "Download client registration triggered in background",
	})
}

// handleTestArrsDownloadClients tests the connection from ARR instances to AltMount
func (s *Server) handleTestArrsDownloadClients(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return c.Status(503).JSON(fiber.Map{
			"success": false,
			"message": "Arrs not available",
		})
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "User not authenticated or no API key available",
		})
	}

	// Get configured host/port or use default
	host := "altmount"
	port := 8080
	urlBase := "sabnzbd"
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		if cfg.SABnzbd.DownloadClientBaseURL != "" {
			rawURL := cfg.SABnzbd.DownloadClientBaseURL
			if !strings.Contains(rawURL, "://") {
				rawURL = "http://" + rawURL
			}
			if u, err := url.Parse(rawURL); err == nil {
				host = u.Hostname()
				if host == "" {
					host = "altmount"
				}
				if p := u.Port(); p != "" {
					if portVal, err := strconv.Atoi(p); err == nil {
						port = portVal
					}
				} else if u.Scheme == "https" {
					port = 443
				} else if u.Scheme == "http" {
					port = 80
				}
				if u.Path != "" && u.Path != "/" {
					urlBase = strings.Trim(u.Path, "/")
				}
			}
		} else {
			port = cfg.WebDAV.Port
		}
	}

	results, err := s.arrsService.TestDownloadClientRegistration(c.Context(), host, port, urlBase, apiKey)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{
			"success": false,
			"message": "Failed to test connections",
			"error":   err.Error(),
		})
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    results,
	})
}
