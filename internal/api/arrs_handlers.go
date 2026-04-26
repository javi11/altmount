package api

import (
	"bytes"
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
	"github.com/javi11/altmount/internal/arrs/model"
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
	Artist struct {
		Path string `json:"path"`
	} `json:"artist"`
	Album struct {
		Title string `json:"title"`
	} `json:"album"`
	Author struct {
		Path string `json:"path"`
	} `json:"author"`
	Book struct {
		Title string `json:"title"`
	} `json:"book"`

	EventType string `json:"eventType"`
	InstanceName string `json:"instanceName,omitempty"`
	FilePath  string `json:"filePath,omitempty"`
	// For upgrades/renames, the file path might be in other fields or need to be inferred
	Movie struct {
		Id         int64  `json:"id"`
		TmdbId     int64  `json:"tmdbId"`
		FolderPath string `json:"folderPath"`
	} `json:"movie"`
	MovieFile struct {
		Id        int64  `json:"id"`
		SceneName string `json:"sceneName"`
		Path      string `json:"path"`
	} `json:"movieFile"`
	Series struct {
		Id     int64  `json:"id"`
		TvdbId int64  `json:"tvdbId"`
		Path   string `json:"path"`
	} `json:"series"`
	EpisodeFile struct {
		Id        int64  `json:"id"`
		SceneName string `json:"sceneName"`
		Path      string `json:"path"`
	} `json:"episodeFile"`
	DeletedFiles ArrsDeletedFiles `json:"deletedFiles,omitempty"`
}

func (req ArrsWebhookRequest) ToMetadata() model.WebhookMetadata {
	meta := model.WebhookMetadata{
		EventType:    req.EventType,
		InstanceName: req.InstanceName,
	}

	if req.Movie.Id > 0 || req.Movie.TmdbId > 0 {
		meta.Movie = &model.MovieMetadata{
			Id:     req.Movie.Id,
			TmdbId: req.Movie.TmdbId,
		}
	}

	if req.MovieFile.Id > 0 || req.MovieFile.SceneName != "" {
		meta.MovieFile = &model.MovieFileMetadata{
			Id:        req.MovieFile.Id,
			SceneName: req.MovieFile.SceneName,
		}
	}

	if req.Series.Id > 0 || req.Series.TvdbId > 0 {
		meta.Series = &model.SeriesMetadata{
			Id:     req.Series.Id,
			TvdbId: req.Series.TvdbId,
		}
	}

	if req.EpisodeFile.Id > 0 || req.EpisodeFile.SceneName != "" {
		meta.EpisodeFile = &model.EpisodeFileMetadata{
			Id:        req.EpisodeFile.Id,
			SceneName: req.EpisodeFile.SceneName,
		}
	}

	return meta
}

type ArrsDeletedFile struct {
	Path string `json:"path"`
}

type ArrsDeletedFiles []ArrsDeletedFile

func (df *ArrsDeletedFiles) UnmarshalJSON(data []byte) error {
	trimmedData := bytes.TrimSpace(data)
	if bytes.Equal(trimmedData, []byte("false")) ||
		bytes.Equal(trimmedData, []byte("null")) ||
		bytes.Equal(trimmedData, []byte("true")) {
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

// handleArrsWebhook handles webhooks from Radarr/Sonarr/Lidarr/Readarr/Whisparr
//
//	@Summary		ARR webhook receiver
//	@Description	Receives file-import webhook events from ARR instances and triggers health checks. Authenticated via API key query parameter.
//	@Tags			ARRs
//	@Accept			json
//	@Produce		json
//	@Param			apikey	query		string				true	"AltMount API key"
//	@Param			body	body		ArrsWebhookRequest	true	"Webhook payload"
//	@Success		200		{object}	APIResponse
//	@Failure		401		{object}	APIResponse
//	@Security		ApiKeyAuth
//	@Router			/arrs/webhook [post]
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
		return RespondServiceUnavailable(c, "Arrs not available", "")
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
	isScanEvent := false

	switch req.EventType {
	case "Test":
		slog.InfoContext(c.Context(), "Received ARR test webhook")
		return c.Status(200).JSON(fiber.Map{"success": true, "message": "Test successful"})
	case "Download", "AlbumImport", "BookImport": // OnImport
		isScanEvent = true
		if req.EpisodeFile.Path != "" {
			pathsToScan = append(pathsToScan, req.EpisodeFile.Path)
		} else if req.MovieFile.Path != "" {
			pathsToScan = append(pathsToScan, req.MovieFile.Path)
		} else if req.FilePath != "" {
			pathsToScan = append(pathsToScan, req.FilePath)
		}
	case "Rename":
		isScanEvent = true
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
		isScanEvent = true
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
	case "MovieDelete", "ArtistDelete", "AuthorDelete":
		if req.Movie.FolderPath != "" {
			dirsToDelete = append(dirsToDelete, req.Movie.FolderPath)
		} else if req.Artist.Path != "" {
			dirsToDelete = append(dirsToDelete, req.Artist.Path)
		} else if req.Author.Path != "" {
			dirsToDelete = append(dirsToDelete, req.Author.Path)
		}
	case "SeriesDelete":
		if req.Series.Path != "" {
			dirsToDelete = append(dirsToDelete, req.Series.Path)
		}
	case "MovieFileDelete":
		if req.MovieFile.Path != "" {
			filesToDelete = append(filesToDelete, req.MovieFile.Path)
		}
	case "EpisodeFileDelete":
		if req.EpisodeFile.Path != "" {
			filesToDelete = append(filesToDelete, req.EpisodeFile.Path)
		}
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

		// If it's a symlink, try to resolve it to the mount path
		if info, err := os.Lstat(path); err == nil && info.Mode()&os.ModeSymlink != 0 {
			if target, err := os.Readlink(path); err == nil {
				// Make target absolute if it's relative
				if !filepath.IsAbs(target) {
					target = filepath.Join(filepath.Dir(path), target)
				}
				cleanTarget := filepath.Clean(target)
				// If symlink target is inside mountPath, use it for normalization
				if mountPath != "" && strings.HasPrefix(cleanTarget, mountPath) {
					normalizedPath = cleanTarget
				}
			}
		}

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
	deleteSourceNzb := cfg.Metadata.ShouldDeleteSourceNzb()

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

		// Delete health record — try by library_path first (more reliable), fall back to file_path
		metadataPath := normalizedPath
		if s.healthRepo != nil {
			if filePath, err := s.healthRepo.DeleteHealthRecordByLibraryPath(c.Context(), path); err == nil {
				slog.InfoContext(c.Context(), "Deleted health record by library_path",
					"library_path", path, "file_path", filePath)
				metadataPath = filePath
			} else {
				// Fall back to normalized file_path lookup
				if err := s.healthRepo.DeleteHealthRecord(c.Context(), normalizedPath); err != nil {
					if strings.Contains(err.Error(), "no health record found") {
						slog.DebugContext(c.Context(), "No health record found to delete from webhook", "path", normalizedPath)
					} else {
						slog.ErrorContext(c.Context(), "Failed to delete health record from webhook", "path", normalizedPath, "error", err)
					}
				}
			}
		}

		// Delete metadata (and optionally source NZB)
		if s.metadataService != nil {
			if err := s.metadataService.DeleteFileMetadataWithSourceNzb(c.Context(), metadataPath, deleteSourceNzb); err != nil {
				slog.DebugContext(c.Context(), "Failed to delete metadata from webhook (might be gone)", "path", metadataPath, "error", err)
			}
		}

		// Redundant Deletion Guard: ensure the file is gone from the local mount
		if s.configManager != nil && metadataPath != "" && metadataPath != "." && metadataPath != "/" {
			cfg := s.configManager.GetConfig()
			if cfg.MountPath != "" {
				localPath := filepath.Join(cfg.MountPath, metadataPath)

				// HARD SAFETY: Never delete the mount root or critical system paths
				cleanLocal := filepath.Clean(localPath)
				if cleanLocal == "/" || cleanLocal == "." || cleanLocal == filepath.Clean(cfg.MountPath) {
					slog.WarnContext(c.Context(), "Safety Guard: Blocked attempt to delete root mount path", "path", cleanLocal)
					continue
				}
				if _, err := os.Stat(localPath); err == nil {
					slog.InfoContext(c.Context(), "Redundant Deletion Guard: Manual removal of ghost file from mount", "path", localPath)
					_ = os.Remove(localPath)
				}
			}
		}
	}

	// Process Directory Deletions
	for _, path := range dirsToDelete {
		normalizedPath := normalize(path)
		slog.InfoContext(c.Context(), "Processing webhook directory deletion",
			"original_path", path,
			"normalized_path", normalizedPath)

		// Delete health records — try by library_path first, fall back to file_path prefix
		var metadataPaths []string
		if s.healthRepo != nil {
			if filePaths, count, err := s.healthRepo.DeleteHealthRecordsByLibraryPathPrefix(c.Context(), path); err != nil {
				slog.ErrorContext(c.Context(), "Failed to delete health records by library_path prefix from webhook", "prefix", path, "error", err)
			} else if count > 0 {
				slog.InfoContext(c.Context(), "Deleted health records by library_path prefix", "prefix", path, "count", count)
				metadataPaths = filePaths
			}

			// Fall back to file_path prefix if no records found by library_path
			if len(metadataPaths) == 0 {
				if count, err := s.healthRepo.DeleteHealthRecordsByPrefix(c.Context(), normalizedPath); err != nil {
					slog.ErrorContext(c.Context(), "Failed to delete health records by prefix from webhook", "prefix", normalizedPath, "error", err)
				} else {
					slog.InfoContext(c.Context(), "Deleted health records for directory", "prefix", normalizedPath, "count", count)
				}
			}
		}

		// Delete metadata directories for each resolved file_path
		if s.metadataService != nil {
			if len(metadataPaths) > 0 {
				for _, mp := range metadataPaths {
					if err := s.metadataService.DeleteFileMetadataWithSourceNzb(c.Context(), mp, deleteSourceNzb); err != nil {
						slog.DebugContext(c.Context(), "Failed to delete metadata from webhook (might be gone)", "path", mp, "error", err)
					}
				}
			} else {
				if err := s.metadataService.DeleteDirectory(normalizedPath); err != nil {
					slog.DebugContext(c.Context(), "Failed to delete metadata directory from webhook (might be gone)", "path", normalizedPath, "error", err)
				}
			}
		}

	}

	if len(pathsToScan) == 0 {
		if isScanEvent {
			slog.WarnContext(c.Context(), "No file path found in webhook payload to scan")
		}
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
				var metadataStr *string
				metaBytes, err := json.Marshal(req.ToMetadata())
				if err == nil {
					str := string(metaBytes)
					metadataStr = &str
				}

				if relinked, err := s.healthRepo.RelinkFileByFilename(c.Context(), fileName, normalizedPath, path, metadataStr); err == nil && relinked {
					attrs := []any{
						"event", req.EventType,
						"instance", req.InstanceName,
						"filename", fileName,
						"new_library_path", path,
					}
					if req.Series.Id > 0 {
						attrs = append(attrs, "series_id", req.Series.Id)
					}
					if req.EpisodeFile.Id > 0 {
						attrs = append(attrs, "episode_file_id", req.EpisodeFile.Id)
					}
					if req.Movie.Id > 0 {
						attrs = append(attrs, "movie_id", req.Movie.Id)
					}
					if req.MovieFile.Id > 0 {
						attrs = append(attrs, "movie_file_id", req.MovieFile.Id)
					}

					slog.InfoContext(c.Context(), "Successfully re-linked health record during webhook with rich metadata", attrs...)
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

			var metadataStr *string
			metaBytes, err := json.Marshal(req.ToMetadata())
			if err == nil {
				str := string(metaBytes)
				metadataStr = &str
			}

			// Add to health check (pending status) with high priority (Next) to ensure it's processed right away
			cfg := s.configManager.GetConfigGetter()()
			err = s.healthRepo.AddFileToHealthCheckWithMetadata(c.Context(), normalizedPath, &path, cfg.GetMaxRetries(), cfg.GetMaxRepairRetries(), sourceNzb, database.HealthPriorityNext, releaseDate, metadataStr)
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
	TotalLidarr      int     `json:"total_lidarr"`
	EnabledLidarr    int     `json:"enabled_lidarr"`
	TotalReadarr     int     `json:"total_readarr"`
	EnabledReadarr   int     `json:"enabled_readarr"`
	TotalWhisparr    int     `json:"total_whisparr"`
	EnabledWhisparr  int     `json:"enabled_whisparr"`
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
//
//	@Summary		List ARR instances
//	@Description	Returns all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/instances [get]
func (s *Server) handleListArrsInstances(c *fiber.Ctx) error {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "Arrs service is not available")
		return RespondServiceUnavailable(c, "Arrs not available", "")
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
//
//	@Summary		Get ARR instance
//	@Description	Returns a specific ARR instance by type and name.
//	@Tags			ARRs
//	@Produce		json
//	@Param			type	path		string	true	"Instance type (sonarr, radarr, lidarr, readarr, or whisparr)"
//	@Param			name	path		string	true	"Instance name"
//	@Success		200		{object}	APIResponse
//	@Failure		404		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/instances/{type}/{name} [get]
func (s *Server) handleGetArrsInstance(c *fiber.Ctx) error {
	if s.arrsService == nil {
		slog.ErrorContext(c.Context(), "Arrs service is not available")
		return RespondServiceUnavailable(c, "Arrs not available", "")
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
//
//	@Summary		Test ARR connection
//	@Description	Tests connectivity to an ARR instance with given credentials.
//	@Tags			ARRs
//	@Accept			json
//	@Produce		json
//	@Param			body	body		ArrsInstanceRequest	true	"Instance connection details"
//	@Success		200		{object}	APIResponse
//	@Failure		400		{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/instances/test [post]
func (s *Server) handleTestArrsConnection(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
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
//
//	@Summary		Get ARR statistics
//	@Description	Returns sync statistics for all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/stats [get]
func (s *Server) handleGetArrsStats(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
	}

	// Get all instances from configuration
	instances := s.arrsService.GetAllInstances()

	// Calculate stats from instances
	var totalRadarr, enabledRadarr, totalSonarr, enabledSonarr int
	var totalLidarr, enabledLidarr, totalReadarr, enabledReadarr, totalWhisparr, enabledWhisparr int
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
		case "lidarr":
			totalLidarr++
			if instance.Enabled {
				enabledLidarr++
			}
		case "readarr":
			totalReadarr++
			if instance.Enabled {
				enabledReadarr++
			}
		case "whisparr":
			totalWhisparr++
			if instance.Enabled {
				enabledWhisparr++
			}
		}
	}

	response := &ArrsStatsResponse{
		TotalInstances:   totalRadarr + totalSonarr + totalLidarr + totalReadarr + totalWhisparr,
		EnabledInstances: enabledRadarr + enabledSonarr + enabledLidarr + enabledReadarr + enabledWhisparr,
		TotalRadarr:      totalRadarr,
		EnabledRadarr:    enabledRadarr,
		TotalSonarr:      totalSonarr,
		EnabledSonarr:    enabledSonarr,
		TotalLidarr:      totalLidarr,
		EnabledLidarr:    enabledLidarr,
		TotalReadarr:     totalReadarr,
		EnabledReadarr:   enabledReadarr,
		TotalWhisparr:    totalWhisparr,
		EnabledWhisparr:  enabledWhisparr,
		DueForSync:       0, // Not applicable with config-first approach
	}

	return c.Status(200).JSON(fiber.Map{
		"success": true,
		"data":    response,
	})
}

// handleGetArrsHealth returns health checks from all ARR instances
//
//	@Summary		Get ARR health
//	@Description	Returns health check results from all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/health [get]
func (s *Server) handleGetArrsHealth(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
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
//
//	@Summary		Register ARR webhooks
//	@Description	Automatically registers AltMount as a webhook connection in all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		500	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/webhook/register [post]
func (s *Server) handleRegisterArrsWebhooks(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return RespondUnauthorized(c, "User not authenticated or no API key available", "")
	}

	// Get configured base URL or use default
	var baseURL string
	if s.configManager != nil {
		cfg := s.configManager.GetConfig()
		baseURL = cfg.GetWebhookBaseURL()
	} else {
		baseURL = "http://altmount:8080" // Fallback if no config manager is available
	}

	if err := s.arrsService.EnsureWebhookRegistration(c.Context(), baseURL, apiKey); err != nil {
		return RespondInternalError(c, "Failed to register webhooks", err.Error())
	}

	return RespondMessage(c, "Webhooks registered successfully")
}

// handleRegisterArrsDownloadClients triggers automatic registration of AltMount as a download client in ARR instances
//
//	@Summary		Register ARR download clients
//	@Description	Automatically registers AltMount as a download client (SABnzbd-compatible) in all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Failure		500	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/download-client/register [post]
func (s *Server) handleRegisterArrsDownloadClients(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "User not authenticated or no API key available",
		})
	}

	// Get configured host/port or use defaults from WebDAV config
	cfg := s.configManager.GetConfig()
	host := cfg.WebDAV.Host
	if host == "" {
		host = "altmount"
	}
	port := cfg.WebDAV.Port
	if port == 0 {
		port = 8080
	}
	urlBase := "sabnzbd"

	if rawURL := cfg.GetDownloadClientBaseURL(); rawURL != "" {
		if !strings.Contains(rawURL, "://") {
			rawURL = "http://" + rawURL
		}
		if u, err := url.Parse(rawURL); err == nil {
			if h := u.Hostname(); h != "" {
				host = h
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
//
//	@Summary		Test ARR download clients
//	@Description	Tests whether AltMount is reachable as a download client from all configured ARR instances.
//	@Tags			ARRs
//	@Produce		json
//	@Success		200	{object}	APIResponse
//	@Security		BearerAuth
//	@Security		ApiKeyAuth
//	@Router			/arrs/download-client/test [post]
func (s *Server) handleTestArrsDownloadClients(c *fiber.Ctx) error {
	if s.arrsService == nil {
		return RespondServiceUnavailable(c, "Arrs not available", "")
	}

	apiKey := s.getAPIKeyForConfig(c)
	if apiKey == "" {
		return c.Status(401).JSON(fiber.Map{
			"success": false,
			"message": "User not authenticated or no API key available",
		})
	}

	// Get configured host/port or use defaults from WebDAV config
	cfg := s.configManager.GetConfig()
	host := cfg.WebDAV.Host
	if host == "" {
		host = "altmount"
	}
	port := cfg.WebDAV.Port
	if port == 0 {
		port = 8080
	}
	urlBase := "sabnzbd"

	if rawURL := cfg.GetDownloadClientBaseURL(); rawURL != "" {
		if !strings.Contains(rawURL, "://") {
			rawURL = "http://" + rawURL
		}
		if u, err := url.Parse(rawURL); err == nil {
			if h := u.Hostname(); h != "" {
				host = h
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
