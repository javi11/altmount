package scanner

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/data"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/lidarr"
	"golift.io/starr/radarr"
	"golift.io/starr/readarr"
	"golift.io/starr/sonarr"
	)

type Manager struct {
	configGetter config.ConfigGetter
	instances    *instances.Manager
	clients      *clients.Manager
	data         *data.Manager
	sf           singleflight.Group
}

func NewManager(configGetter config.ConfigGetter, instances *instances.Manager, clients *clients.Manager, data *data.Manager) *Manager {
	return &Manager{
		configGetter: configGetter,
		instances:    instances,
		clients:      clients,
		data:         data,
	}
}

// findInstanceForFilePath finds which ARR instance manages the given file path
func (m *Manager) findInstanceForFilePath(ctx context.Context, filePath string, relativePath string) (instanceType string, instanceName string, err error) {
	slog.DebugContext(ctx, "Finding instance for file path", "file_path", filePath, "relative_path", relativePath)

	allInstances := m.instances.GetAllInstances()

	// Strategy 1: Fast Path - Check Root Folders
	for _, instance := range allInstances {
		if !instance.Enabled {
			continue
		}

		if client, err := m.clients.GetOrCreateClient(instance); err == nil {
			if m.managesFile(ctx, instance.Type, client, filePath) {
				return instance.Type, instance.Name, nil
			}
		}
	}

	// Strategy 2: Category Match - Check if file is in the staging/complete folder
	cfg := m.configGetter()
	if cfg.SABnzbd.CompleteDir != "" {
		// Normalize completeDir to a segment like "/complete/"
		completeDir := strings.Trim(filepath.ToSlash(cfg.SABnzbd.CompleteDir), "/")
		completeSegment := "/" + completeDir + "/"
		normalizedPath := filepath.ToSlash(filePath)

		// Check if path contains the complete directory as a segment
		if _, after, ok := strings.Cut(normalizedPath, completeSegment); ok {
			// Extract everything after the complete directory segment (e.g., "tv/show/file.mkv")
			afterPrefix := after
			parts := strings.Split(afterPrefix, "/")
			if len(parts) > 0 {
				category := parts[0]
				slog.DebugContext(ctx, "File is in complete_dir, matching by category", "category", category)

				for _, instance := range allInstances {
					if !instance.Enabled {
						continue
					}

					if strings.EqualFold(instance.Category, category) {
						slog.InfoContext(ctx, "Found managing instance by category in complete_dir", "instance", instance.Name, "category", category)
						return instance.Type, instance.Name, nil
					}
				}
			}
		}
	}

	// Strategy 3: Slow Path - Search Cache by Relative Path
	if relativePath != "" {
		slog.InfoContext(ctx, "Root folder match failed, attempting relative path search", "relative_path", relativePath)

		for _, instance := range allInstances {
			if !instance.Enabled {
				continue
			}

			if client, err := m.clients.GetOrCreateClient(instance); err == nil {
				if m.hasFile(ctx, instance.Type, client, instance.Name, relativePath) {
					slog.InfoContext(ctx, "Found managing instance by relative path", "instance", instance.Name, "type", instance.Type)
					return instance.Type, instance.Name, nil
				}
			}
		}
	}

	return "", "", fmt.Errorf("no ARR instance found managing file path: %s", filePath)
}

func (m *Manager) managesFile(ctx context.Context, instanceType string, client any, filePath string) bool {
	switch instanceType {
	case "radarr":
		rc, ok := client.(*radarr.Radarr)
		if !ok {
			return false
		}
		return m.radarrManagesFile(ctx, rc, filePath)
	case "sonarr":
		sc, ok := client.(*sonarr.Sonarr)
		if !ok {
			return false
		}
		return m.sonarrManagesFile(ctx, sc, filePath)
	case "lidarr":
		lc, ok := client.(*lidarr.Lidarr)
		if !ok {
			return false
		}
		return m.lidarrManagesFile(ctx, lc, filePath)
	case "readarr":
		rc, ok := client.(*readarr.Readarr)
		if !ok {
			return false
		}
		return m.readarrManagesFile(ctx, rc, filePath)
	case "whisparr":
		wc, ok := client.(*radarr.Radarr)
		if !ok {
			return false
		}
		return m.radarrManagesFile(ctx, wc, filePath)
	default:
		return false
	}
}

func (m *Manager) hasFile(ctx context.Context, instanceType string, client any, instanceName, relativePath string) bool {
	switch instanceType {
	case "radarr":
		rc, ok := client.(*radarr.Radarr)
		if !ok {
			return false
		}
		return m.radarrHasFile(ctx, rc, instanceName, relativePath)
	case "sonarr":
		sc, ok := client.(*sonarr.Sonarr)
		if !ok {
			return false
		}
		return m.sonarrHasFile(ctx, sc, instanceName, relativePath)
	case "lidarr", "readarr", "whisparr":
		// For now, these don't have a slow path search implementation
		// They rely on the Root Folder (Strategy 1) or Category (Strategy 2)
		return false
	default:
		return false
	}
}

// radarrManagesFile checks if Radarr manages the given file path using root folders (checkrr approach)
func (m *Manager) radarrManagesFile(ctx context.Context, client *radarr.Radarr, filePath string) bool {
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
		// Check for direct prefix match or if the filePath contains the folder.Path (common in Docker/Remote setups)
		if strings.HasPrefix(filePath, folder.Path) {
			slog.DebugContext(ctx, "File matches Radarr root folder", "folder_path", folder.Path)
			return true
		}
	}

	slog.DebugContext(ctx, "File does not match any Radarr root folders")
	return false
}

// sonarrManagesFile checks if Sonarr manages the given file path using root folders (checkrr approach)
func (m *Manager) sonarrManagesFile(ctx context.Context, client *sonarr.Sonarr, filePath string) bool {
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

// lidarrManagesFile checks if Lidarr manages the given file path using root folders
func (m *Manager) lidarrManagesFile(ctx context.Context, client *lidarr.Lidarr, filePath string) bool {
	slog.DebugContext(ctx, "Checking Lidarr root folders for file ownership", "file_path", filePath)
	rootFolders, err := client.GetRootFoldersContext(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Failed to get root folders from Lidarr", "error", err)
		return false
	}
	for _, folder := range rootFolders {
		if strings.HasPrefix(filePath, folder.Path) {
			return true
		}
	}
	return false
}

// readarrManagesFile checks if Readarr manages the given file path using root folders
func (m *Manager) readarrManagesFile(ctx context.Context, client *readarr.Readarr, filePath string) bool {
	slog.DebugContext(ctx, "Checking Readarr root folders for file ownership", "file_path", filePath)
	rootFolders, err := client.GetRootFoldersContext(ctx)
	if err != nil {
		slog.DebugContext(ctx, "Failed to get root folders from Readarr", "error", err)
		return false
	}
	for _, folder := range rootFolders {
		if strings.HasPrefix(filePath, folder.Path) {
			return true
		}
	}
	return false
}

// radarrHasFile checks if any movie in the instance contains the given relative path
func (m *Manager) radarrHasFile(ctx context.Context, client *radarr.Radarr, instanceName, relativePath string) bool {
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get movies for relative path check", "instance", instanceName, "error", err)
		return false
	}

	strippedRelative := strings.TrimSuffix(relativePath, ".strm")

	for _, movie := range movies {
		if movie.HasFile && movie.MovieFile != nil {
			if strings.HasSuffix(movie.MovieFile.Path, relativePath) ||
				strings.HasSuffix(strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)), strippedRelative) {
				return true
			}
		}
	}
	return false
}

// sonarrHasFile checks if any series in the instance contains the given relative path
func (m *Manager) sonarrHasFile(ctx context.Context, client *sonarr.Sonarr, instanceName, relativePath string) bool {
	seriesList, err := m.data.GetSeries(ctx, client, instanceName)
	if err != nil {
		slog.ErrorContext(ctx, "Failed to get series for relative path check", "instance", instanceName, "error", err)
		return false
	}

	// Normalize relative path for comparison
	relativePath = filepath.ToSlash(relativePath)
	strippedRelative := strings.TrimSuffix(relativePath, ".strm")

	for _, series := range seriesList {
		// Check if the series folder name is part of the relative path
		folderName := filepath.Base(series.Path)
		if strings.Contains(relativePath, folderName) || strings.Contains(strippedRelative, folderName) {
			return true
		}
	}
	return false
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
func (m *Manager) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string, downloadID string, sourceNzbPath *string, reason string) error {
	res, err, _ := m.sf.Do(fmt.Sprintf("rescan:%s", pathForRescan), func() (interface{}, error) {
		slog.InfoContext(ctx, "Triggering ARR rescan", "path", pathForRescan, "relative_path", relativePath, "download_id", downloadID, "reason", reason)

		// Find which ARR instance manages this file path
		instanceType, instanceName, err := m.findInstanceForFilePath(ctx, pathForRescan, relativePath)
		if err != nil {
			return nil, fmt.Errorf("failed to find ARR instance for file path %s: %w", pathForRescan, err)
		}

		// Find the instance configuration
		instanceConfig, err := m.instances.FindConfigInstance(instanceType, instanceName)
		if err != nil {
			return nil, fmt.Errorf("failed to find instance config: %w", err)
		}

		// Check if instance is enabled
		if !instanceConfig.Enabled {
			return nil, fmt.Errorf("instance %s/%s is disabled", instanceType, instanceName)
		}

		// Trigger rescan based on instance type
		switch instanceType {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to create Radarr client: %w", err)
			}
			return nil, m.triggerRadarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName, downloadID, sourceNzbPath, reason)

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
			if err != nil {
				return nil, fmt.Errorf("failed to create Sonarr client: %w", err)
			}
			return nil, m.triggerSonarrRescanByPath(ctx, client, pathForRescan, relativePath, instanceName, downloadID, sourceNzbPath, reason)

		case "lidarr", "readarr", "whisparr":
			// For now, we only support RefreshMonitoredDownloads for these
			m.TriggerScanForFile(ctx, pathForRescan)
			return nil, nil

		default:
			return nil, fmt.Errorf("unsupported instance type: %s", instanceType)
		}
	})

	if err != nil {
		return err
	}
	if res != nil {
		return res.(error)
	}
	return nil
}

// TriggerScanForFile finds the ARR instance managing the file and triggers a download scan on it.
func (m *Manager) TriggerScanForFile(ctx context.Context, filePath string) error {
	// Try to find which instance manages this file path
	instanceType, instanceName, err := m.findInstanceForFilePath(ctx, filePath, "")
	if err != nil {
		return err
	}

	instance, err := m.instances.FindConfigInstance(instanceType, instanceName)
	if err != nil {
		return err
	}

	if !instance.Enabled {
		return fmt.Errorf("instance %s is disabled", instanceName)
	}

	slog.InfoContext(ctx, "Triggering download scan for specific instance", "instance", instanceName, "type", instanceType)

	// Launch scan in background to not block caller
	go func() {
		bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		switch instance.Type {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to create Radarr client for scan trigger", "instance", instance.Name, "error", err)
				return
			}
			// Trigger RefreshMonitoredDownloads
			_, err = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", instance.Name, "error", err)
			} else {
				slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", instance.Name)
			}

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to create Sonarr client for scan trigger", "instance", instance.Name, "error", err)
				return
			}
			// Trigger RefreshMonitoredDownloads
			_, err = client.SendCommandContext(bgCtx, &sonarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			if err != nil {
				slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", instance.Name, "error", err)
			} else {
				slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", instance.Name)
			}
		case "lidarr":
			client, err := m.clients.GetOrCreateLidarrClient(instance.Name, instance.URL, instance.APIKey)
			if err == nil {
				_, _ = client.SendCommandContext(bgCtx, &lidarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			}
		case "readarr":
			client, err := m.clients.GetOrCreateReadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err == nil {
				_, _ = client.SendCommandContext(bgCtx, &readarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			}
		case "whisparr":
			client, err := m.clients.GetOrCreateWhisparrClient(instance.Name, instance.URL, instance.APIKey)
			if err == nil {
				_, _ = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
			}
		}
	}()

	return nil
}

// TriggerDownloadScan triggers the "Check For Finished Downloads" task in ARR instances
func (m *Manager) TriggerDownloadScan(ctx context.Context, instanceType string) {
	instances := m.instances.GetAllInstances()
	for _, instance := range instances {
		if !instance.Enabled || instance.Type != instanceType {
			continue
		}

		slog.DebugContext(ctx, "Triggering download client scan", "instance", instance.Name, "type", instance.Type)

		go func(inst *model.ConfigInstance) {
			_, _, _ = m.sf.Do(fmt.Sprintf("scan:%s", inst.Name), func() (interface{}, error) {
				bgCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()

				switch inst.Type {
				case "radarr":
					client, err := m.clients.GetOrCreateRadarrClient(inst.Name, inst.URL, inst.APIKey)
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to create Radarr client for scan trigger", "instance", inst.Name, "error", err)
						return nil, err
					}
					// Trigger RefreshMonitoredDownloads
					_, err = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", inst.Name, "error", err)
					} else {
						slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", inst.Name)
					}

				case "sonarr":
					client, err := m.clients.GetOrCreateSonarrClient(inst.Name, inst.URL, inst.APIKey)
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to create Sonarr client for scan trigger", "instance", inst.Name, "error", err)
						return nil, err
					}
					// Trigger RefreshMonitoredDownloads
					_, err = client.SendCommandContext(bgCtx, &sonarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					if err != nil {
						slog.ErrorContext(bgCtx, "Failed to trigger RefreshMonitoredDownloads", "instance", inst.Name, "error", err)
					} else {
						slog.InfoContext(bgCtx, "Triggered RefreshMonitoredDownloads", "instance", inst.Name)
					}
				case "lidarr":
					client, err := m.clients.GetOrCreateLidarrClient(inst.Name, inst.URL, inst.APIKey)
					if err == nil {
						_, _ = client.SendCommandContext(bgCtx, &lidarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					}
				case "readarr":
					client, err := m.clients.GetOrCreateReadarrClient(inst.Name, inst.URL, inst.APIKey)
					if err == nil {
						_, _ = client.SendCommandContext(bgCtx, &readarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					}
				case "whisparr":
					client, err := m.clients.GetOrCreateWhisparrClient(inst.Name, inst.URL, inst.APIKey)
					if err == nil {
						_, _ = client.SendCommandContext(bgCtx, &radarr.CommandRequest{Name: "RefreshMonitoredDownloads"})
					}
				}
				return nil, nil
			})
		}(instance)
	}
}

func (m *Manager) matchRadarrMovie(ctx context.Context, movie *radarr.Movie, filePath, relativePath string) bool {
	requestFileName := filepath.Base(filePath)

	if movie.HasFile && movie.MovieFile != nil {
		// Try exact match
		if movie.MovieFile.Path == filePath {
			return true
		}

		movieFileName := filepath.Base(movie.MovieFile.Path)
		if movieFileName == requestFileName {
			return true
		}

		// Try match without .strm extension if filePath is a .strm file
		if before, ok := strings.CutSuffix(filePath, ".strm"); ok {
			strippedPath := before
			// Check if movie file path (without its own extension) matches stripped filePath
			if strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)) == strippedPath {
				return true
			}
		}

		// Try suffix match with relative path if provided
		if relativePath != "" {
			strippedRelative := strings.TrimSuffix(relativePath, ".strm")
			if strings.HasSuffix(movie.MovieFile.Path, relativePath) ||
				strings.HasSuffix(strings.TrimSuffix(movie.MovieFile.Path, filepath.Ext(movie.MovieFile.Path)), strippedRelative) {
				return true
			}
		}
	}
	return false
}

func (m *Manager) matchSonarrSeries(ctx context.Context, show *sonarr.Series, filePath, relativePath string) bool {
	// Try root path match
	if strings.HasPrefix(filePath, show.Path) {
		return true
	}

	// Try relative path match
	if relativePath != "" {
		strippedRelative := strings.TrimSuffix(relativePath, ".strm")
		// Check if the series folder name is part of the relative path
		folderName := filepath.Base(show.Path)
		if strings.Contains(relativePath, folderName) || strings.Contains(strippedRelative, folderName) {
			return true
		}
	}
	return false
}

func (m *Manager) matchSonarrFile(ctx context.Context, f *sonarr.EpisodeFile, filePath, relativePath string) bool {
	// Try exact match
	if f.Path == filePath {
		return true
	}

	// Try filename match
	requestFileName := filepath.Base(filePath)
	if filepath.Base(f.Path) == requestFileName {
		return true
	}

	// Try match without .strm extension if filePath is a .strm file
	if before, ok := strings.CutSuffix(filePath, ".strm"); ok {
		strippedPath := before
		// Check if episode file path (without its own extension) matches stripped filePath
		if strings.TrimSuffix(f.Path, filepath.Ext(f.Path)) == strippedPath {
			return true
		}
	}

	// Try suffix match with relative path if provided
	if relativePath != "" {
		strippedRelative := strings.TrimSuffix(relativePath, ".strm")
		if strings.HasSuffix(f.Path, relativePath) ||
			strings.HasSuffix(strings.TrimSuffix(f.Path, filepath.Ext(f.Path)), strippedRelative) {
			return true
		}
	}

	return false
}

// GetDownloadID finds the ARR instance managing the file and looks up its DownloadID in history
func (m *Manager) GetDownloadID(ctx context.Context, filePath, relativePath string) (string, error) {
	// Find which ARR instance manages this file path
	instanceType, instanceName, err := m.findInstanceForFilePath(ctx, filePath, relativePath)
	if err != nil {
		return "", fmt.Errorf("failed to find ARR instance for file path %s: %w", filePath, err)
	}

	// Find the instance configuration
	instanceConfig, err := m.instances.FindConfigInstance(instanceType, instanceName)
	if err != nil {
		return "", fmt.Errorf("failed to find config for instance %s/%s: %w", instanceType, instanceName, err)
	}

	if !instanceConfig.Enabled {
		return "", fmt.Errorf("instance %s/%s is disabled", instanceType, instanceName)
	}

	// Lookup download ID based on instance type
	switch instanceType {
	case "radarr":
		client, err := m.clients.GetOrCreateRadarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
		if err != nil {
			return "", fmt.Errorf("failed to create Radarr client: %w", err)
		}
		return m.getRadarrDownloadID(ctx, client, filePath, relativePath, instanceName)

	case "sonarr":
		client, err := m.clients.GetOrCreateSonarrClient(instanceName, instanceConfig.URL, instanceConfig.APIKey)
		if err != nil {
			return "", fmt.Errorf("failed to create Sonarr client: %w", err)
		}
		return m.getSonarrDownloadID(ctx, client, filePath, relativePath, instanceName)

	default:
		return "", fmt.Errorf("unsupported instance type for download ID lookup: %s", instanceType)
	}
}

func (m *Manager) getRadarrDownloadID(ctx context.Context, client *radarr.Radarr, filePath, relativePath, instanceName string) (string, error) {
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err != nil {
		return "", err
	}

	var targetMovie *radarr.Movie
	for _, movie := range movies {
		if m.matchRadarrMovie(ctx, movie, filePath, relativePath) {
			targetMovie = movie
			break
		}
	}

	if targetMovie == nil || !targetMovie.HasFile || targetMovie.MovieFile == nil {
		return "", fmt.Errorf("movie file not found in Radarr library: %s", filePath)
	}

	fileID := strconv.FormatInt(targetMovie.MovieFile.ID, 10)
	const pageSize = 1000
	const maxPages = 5

	for page := 1; page <= maxPages; page++ {
		req := &starr.PageReq{PageSize: pageSize, Page: page, SortKey: "date", SortDir: starr.SortDescend}
		req.Set("movieId", strconv.FormatInt(targetMovie.ID, 10))

		history, err := client.GetHistoryPageContext(ctx, req)
		if err != nil {
			return "", err
		}
		if len(history.Records) == 0 {
			break
		}

		for _, record := range history.Records {
			if record.Data.FileID == fileID && (record.EventType == "movieFileImported" || record.EventType == "downloadFolderImported") {
				return record.DownloadID, nil
			}
		}
	}

	return "", fmt.Errorf("download ID not found in Radarr history for movie file %s", fileID)
}

func (m *Manager) getSonarrDownloadID(ctx context.Context, client *sonarr.Sonarr, filePath, relativePath, instanceName string) (string, error) {
	series, err := m.data.GetSeries(ctx, client, instanceName)
	if err != nil {
		return "", err
	}

	var targetSeries *sonarr.Series
	for _, s := range series {
		if m.matchSonarrSeries(ctx, s, filePath, relativePath) {
			targetSeries = s
			break
		}
	}

	if targetSeries == nil {
		return "", fmt.Errorf("series not found in Sonarr: %s", filePath)
	}

	files, err := m.data.GetEpisodeFiles(ctx, client, instanceName, targetSeries.ID)
	if err != nil {
		return "", err
	}

	var targetFileID int64
	for _, f := range files {
		if m.matchSonarrFile(ctx, f, filePath, relativePath) {
			targetFileID = f.ID
			break
		}
	}

	if targetFileID == 0 {
		return "", fmt.Errorf("episode file not found in Sonarr: %s", filePath)
	}

	fileID := strconv.FormatInt(targetFileID, 10)
	const pageSize = 1000
	const maxPages = 5

	for page := 1; page <= maxPages; page++ {
		req := &starr.PageReq{PageSize: pageSize, Page: page, SortKey: "date", SortDir: starr.SortDescend}
		req.Set("seriesId", strconv.FormatInt(targetSeries.ID, 10))

		history, err := client.GetHistoryPageContext(ctx, req)
		if err != nil {
			return "", err
		}
		if len(history.Records) == 0 {
			break
		}

		for _, record := range history.Records {
			if record.Data.FileID == fileID && record.EventType == "downloadFolderImported" {
				return record.DownloadID, nil
			}
		}
	}

	return "", fmt.Errorf("download ID not found in Sonarr history for episode file %s", fileID)
}

// triggerRadarrRescanByPath triggers a rescan in Radarr for the given file path
func (m *Manager) triggerRadarrRescanByPath(ctx context.Context, client *radarr.Radarr, filePath, relativePath, instanceName string, downloadID string, sourceNzbPath *string, reason string) error {
	slog.InfoContext(ctx, "Searching Radarr for matching movie",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath,
		"download_id", downloadID,
		"reason", reason)

	// Get all movies to find the one with matching file path
	movies, err := m.data.GetMovies(ctx, client, instanceName)
	if err != nil {
		return fmt.Errorf("failed to get movies from Radarr: %w", err)
	}

	var targetMovie *radarr.Movie
	for _, movie := range movies {
		if m.matchRadarrMovie(ctx, movie, filePath, relativePath) {
			targetMovie = movie
			break
		}
	}

	if targetMovie == nil {
		slog.WarnContext(ctx, "No movie found with matching file path in Radarr library, attempting queue-based failure",
			"instance", instanceName,
			"file_path", filePath)

		// Fallback: search in Radarr download queue for active/stuck imports
		if err := m.failRadarrQueueItemByPath(ctx, client, filePath, downloadID, reason); err == nil {
			return nil
		}

		return fmt.Errorf("no movie found with file path %s in library or queue: %w", filePath, model.ErrPathMatchFailed)
	}

	slog.InfoContext(ctx, "Found matching movie for file",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"movie_title", targetMovie.Title,
		"movie_path", targetMovie.Path,
		"file_path", filePath)

	// If we found the movie but it has no file (or different file), we can't blocklist the specific file ID
	// But we can still trigger search
	if targetMovie.HasFile && targetMovie.MovieFile != nil {
		// Try to blocklist the release associated with this file
		if err := m.blocklistRadarrMovieFile(ctx, client, targetMovie.ID, targetMovie.MovieFile.ID, downloadID, sourceNzbPath, reason); err != nil {
			slog.WarnContext(ctx, "Failed to blocklist Radarr release", "error", err)
		}

		// Delete the existing file from Radarr database
		err = client.DeleteMovieFilesContext(ctx, targetMovie.MovieFile.ID)
		if err != nil {
			slog.WarnContext(ctx, "Failed to delete movie file from Radarr, continuing with search",
				"instance", instanceName,
				"movie_id", targetMovie.ID,
				"file_id", targetMovie.MovieFile.ID,
				"error", err)
		}
	} else {
		slog.InfoContext(ctx, "Movie has no file linked in Radarr, skipping blocklist/delete and proceeding to search",
			"movie", targetMovie.Title)
	}

	// Step 3: Trigger targeted search for the missing movie
	searchCmd := &radarr.CommandRequest{
		Name:     "MoviesSearch",
		MovieIDs: []int64{targetMovie.ID},
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Radarr search for movie ID %d: %w", targetMovie.ID, err)
	}

	slog.InfoContext(ctx, "Successfully triggered Radarr targeted search for re-download",
		"instance", instanceName,
		"movie_id", targetMovie.ID,
		"command_id", response.ID)

	return nil
}

// triggerSonarrRescanByPath triggers a rescan in Sonarr for the given file path
func (m *Manager) triggerSonarrRescanByPath(ctx context.Context, client *sonarr.Sonarr, filePath, relativePath, instanceName string, downloadID string, sourceNzbPath *string, reason string) error {
	cfg := m.configGetter()

	// Get library directory from health config
	libraryDir := m.configGetter().MountPath
	if cfg.Health.LibraryDir != nil && *cfg.Health.LibraryDir != "" {
		libraryDir = *cfg.Health.LibraryDir
	}

	slog.InfoContext(ctx, "Searching Sonarr for matching series",
		"instance", instanceName,
		"file_path", filePath,
		"relative_path", relativePath,
		"library_dir", libraryDir,
		"download_id", downloadID,
		"reason", reason)

	// Get all series to find the one that contains this file path
	series, err := m.data.GetSeries(ctx, client, instanceName)
	if err != nil {
		return fmt.Errorf("failed to get series from Sonarr: %w", err)
	}

	// Find the series that contains this file path
	var targetSeries *sonarr.Series
	for _, show := range series {
		if m.matchSonarrSeries(ctx, show, filePath, relativePath) {
			targetSeries = show
			break
		}
	}

	if targetSeries == nil {
		slog.WarnContext(ctx, "No series found in Sonarr matching file path in library, attempting queue-based failure",
			"instance", instanceName,
			"file_path", filePath)

		// Fallback: search in Sonarr download queue for active/stuck imports
		if err := m.failSonarrQueueItemByPath(ctx, client, filePath, downloadID, reason); err == nil {
			return nil
		}

		return fmt.Errorf("no series found containing file path in library or queue: %s", filePath)
	}

	slog.InfoContext(ctx, "Found matching series, searching for episode file",
		"series_title", targetSeries.Title,
		"series_path", targetSeries.Path)

	// Get all episodes for this specific series
	episodes, err := client.GetSeriesEpisodesContext(ctx, &sonarr.GetEpisode{
		SeriesID: targetSeries.ID,
	})
	if err != nil {
		return fmt.Errorf("failed to get episodes for series %s: %w", targetSeries.Title, err)
	}

	// Get episode files for this series to find the matching file
	episodeFiles, err := m.data.GetEpisodeFiles(ctx, client, instanceName, targetSeries.ID)
	if err != nil {
		return fmt.Errorf("failed to get episode files for series %s: %w", targetSeries.Title, err)
	}

	// Find the episode file with matching path
	var targetEpisodeFile *sonarr.EpisodeFile
	for _, f := range episodeFiles {
		if m.matchSonarrFile(ctx, f, filePath, relativePath) {
			targetEpisodeFile = f
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

			// Try to blocklist the release associated with this file
			if err := m.blocklistSonarrEpisodeFile(ctx, client, targetSeries.ID, targetEpisodeFile.ID, downloadID, sourceNzbPath, reason); err != nil {
				slog.WarnContext(ctx, "Failed to blocklist Sonarr release", "error", err)
			}

			// Delete the existing episode file from Sonarr database
			err = client.DeleteEpisodeFileContext(ctx, targetEpisodeFile.ID)
			if err != nil {
				slog.WarnContext(ctx, "Failed to delete episode file from Sonarr, continuing with search",
					"instance", instanceName,
					"episode_file_id", targetEpisodeFile.ID,
					"error", err)
			}
		}
	} else {
		slog.WarnContext(ctx, "Series found but no matching episode file found in Sonarr library, attempting queue-based failure",
			"series", targetSeries.Title,
			"file_path", filePath)

		// Fallback: search in Sonarr download queue
		if err := m.failSonarrQueueItemByPath(ctx, client, filePath, downloadID, reason); err == nil {
			return nil
		}
	}

	if len(episodeIDs) == 0 {
		return fmt.Errorf("no episodes found for file in library or queue: %s: %w", filePath, model.ErrPathMatchFailed)
	}

	// Trigger targeted episode search for all episodes in this file
	searchCmd := &sonarr.CommandRequest{
		Name:       "EpisodeSearch",
		EpisodeIDs: episodeIDs,
	}

	response, err := client.SendCommandContext(ctx, searchCmd)
	if err != nil {
		return fmt.Errorf("failed to trigger Sonarr episode search: %w", err)
	}

	slog.InfoContext(ctx, "Successfully triggered Sonarr targeted episode search for re-download",
		"instance", instanceName,
		"series_title", targetSeries.Title,
		"episode_ids", episodeIDs,
		"command_id", response.ID)

	return nil
}

// failRadarrQueueItemByPath searches for an item in the active Radarr queue by path or downloadID and marks it as failed
func (m *Manager) failRadarrQueueItemByPath(ctx context.Context, client *radarr.Radarr, path, downloadID, reason string) error {
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Radarr queue: %w", err)
	}

	for _, q := range queue.Records {
		// Try match by DownloadID (most reliable) or path
		match := (downloadID != "" && q.DownloadID == downloadID) ||
			q.OutputPath == path ||
			(q.OutputPath != "" && strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(q.OutputPath))) ||
			(q.OutputPath != "" && filepath.Base(q.OutputPath) == filepath.Base(path))

		if match {
			slog.InfoContext(ctx, "Found matching item in ARR download queue, marking as failed",
				"queue_id", q.ID, "path", path, "download_id", downloadID, "output_path", q.OutputPath, "reason", reason)

			removeFromClient := true
			opts := &starr.QueueDeleteOpts{
				RemoveFromClient: &removeFromClient,
				BlockList:        true,
				SkipRedownload:   false,
			}
			return client.DeleteQueueContext(ctx, q.ID, opts)
		}
	}

	return fmt.Errorf("no matching item found in Radarr queue for path: %s", path)
}

// failSonarrQueueItemByPath searches for an item in the active Sonarr queue by path or downloadID and marks it as failed
func (m *Manager) failSonarrQueueItemByPath(ctx context.Context, client *sonarr.Sonarr, path, downloadID, reason string) error {
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return fmt.Errorf("failed to get Sonarr queue: %w", err)
	}

	for _, q := range queue.Records {
		// Try match by DownloadID (most reliable) or path
		match := (downloadID != "" && q.DownloadID == downloadID) ||
			q.OutputPath == path ||
			(q.OutputPath != "" && strings.HasSuffix(filepath.ToSlash(path), filepath.ToSlash(q.OutputPath))) ||
			(q.OutputPath != "" && filepath.Base(q.OutputPath) == filepath.Base(path))

		if match {
			slog.InfoContext(ctx, "Found matching item in ARR download queue, marking as failed",
				"queue_id", q.ID, "path", path, "download_id", downloadID, "output_path", q.OutputPath, "reason", reason)

			removeFromClient := true
			opts := &starr.QueueDeleteOpts{
				RemoveFromClient: &removeFromClient,
				BlockList:        true,
				SkipRedownload:   false,
			}
			return client.DeleteQueueContext(ctx, q.ID, opts)
		}
	}

	return fmt.Errorf("no matching item found in Sonarr queue for path: %s", path)
}

func (m *Manager) blocklistRadarrMovieFile(ctx context.Context, client *radarr.Radarr, movieID int64, fileID int64, knownDownloadID string, sourceNzbPath *string, reason string) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for movie file", "movie_id", movieID, "file_id", fileID, "known_download_id", knownDownloadID)

	var downloadID string = knownDownloadID
	const pageSize = 1000
	const maxPages = 5 // Total 5000 records max

	releaseTitle := ""
	if sourceNzbPath != nil && *sourceNzbPath != "" {
		// Extract release title from NZB filename (e.g. /metadata/nzbs/My.Release.2024.nzb -> My.Release.2024)
		releaseTitle = strings.TrimSuffix(filepath.Base(*sourceNzbPath), filepath.Ext(*sourceNzbPath))
	}

	if downloadID == "" {
		targetFileID := strconv.FormatInt(fileID, 10)

		// 1. Find the import event to get the downloadId
		for page := 1; page <= maxPages; page++ {
			req := &starr.PageReq{
				PageSize: pageSize,
				Page:     page,
				SortKey:  "date",
				SortDir:  starr.SortDescend,
			}
			req.Set("movieId", strconv.FormatInt(movieID, 10))

			history, err := client.GetHistoryPageContext(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to fetch Radarr history page %d: %w", page, err)
			}

			if len(history.Records) == 0 {
				break
			}

			found := false
			for _, record := range history.Records {
				// Match by FileID OR by Release Title (Intelligent Fallback)
				isImportEvent := record.EventType == "movieFileImported" || record.EventType == "downloadFolderImported"
				match := (record.Data.FileID == targetFileID && isImportEvent) ||
					(releaseTitle != "" && strings.EqualFold(record.SourceTitle, releaseTitle))

				if match && record.DownloadID != "" {
					downloadID = record.DownloadID
					found = true
					slog.InfoContext(ctx, "Found matching history record for blocklisting",
						"download_id", downloadID, "source_title", record.SourceTitle, "match_type", "fileID/title")
					break
				}
			}

			if found {
				break
			}
		}

		if downloadID == "" {
			slog.WarnContext(ctx, "Could not find import event in Radarr history for file after checking multiple pages", "movie_id", movieID, "file_id", fileID, "release_title", releaseTitle)
			return nil
		}
	}

	// 2. Find the original grab event using the downloadId
	for page := 1; page <= maxPages; page++ {
		req := &starr.PageReq{
			PageSize: pageSize,
			Page:     page,
			SortKey:  "date",
			SortDir:  starr.SortDescend,
		}
		req.Set("movieId", strconv.FormatInt(movieID, 10))

		history, err := client.GetHistoryPageContext(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to fetch Radarr history page %d: %w", page, err)
		}

		if len(history.Records) == 0 {
			break
		}

		found := false
		for _, record := range history.Records {
			if record.DownloadID == downloadID && record.EventType == "grabbed" {
				slog.InfoContext(ctx, "Found grabbed history record, marking as failed to blocklist release",
					"history_id", record.ID, "download_id", downloadID, "page", page, "reason", reason)
				if failErr := client.FailContext(ctx, record.ID); failErr != nil {
					return fmt.Errorf("failed to fail Radarr grab event %d: %w", record.ID, failErr)
				}
				return nil
			}
		}

		if found {
			return nil
		}
	}

	slog.WarnContext(ctx, "Could not find grab event in Radarr history for download after checking multiple pages", "download_id", downloadID)
	return nil
}

func (m *Manager) blocklistSonarrEpisodeFile(ctx context.Context, client *sonarr.Sonarr, seriesID int64, fileID int64, knownDownloadID string, sourceNzbPath *string, reason string) error {
	slog.DebugContext(ctx, "Attempting to find and blocklist release for episode file", "series_id", seriesID, "file_id", fileID, "known_download_id", knownDownloadID)

	var downloadID string = knownDownloadID
	const pageSize = 1000
	const maxPages = 5 // Total 5000 records max

	releaseTitle := ""
	if sourceNzbPath != nil && *sourceNzbPath != "" {
		// Extract release title from NZB filename
		releaseTitle = strings.TrimSuffix(filepath.Base(*sourceNzbPath), filepath.Ext(*sourceNzbPath))
	}

	if downloadID == "" {
		targetFileID := strconv.FormatInt(fileID, 10)

		// 1. Find the import event to get the downloadId
		for page := 1; page <= maxPages; page++ {
			req := &starr.PageReq{
				PageSize: pageSize,
				Page:     page,
				SortKey:  "date",
				SortDir:  starr.SortDescend,
			}
			req.Set("seriesId", strconv.FormatInt(seriesID, 10))

			history, err := client.GetHistoryPageContext(ctx, req)
			if err != nil {
				return fmt.Errorf("failed to fetch Sonarr history page %d: %w", page, err)
			}

			if len(history.Records) == 0 {
				break
			}

			found := false
			for _, record := range history.Records {
				// Match by FileID OR by Release Title (Intelligent Fallback)
				isImportEvent := record.EventType == "downloadFolderImported"
				match := (record.Data.FileID == targetFileID && isImportEvent) ||
					(releaseTitle != "" && strings.EqualFold(record.SourceTitle, releaseTitle))

				if match && record.DownloadID != "" {
					downloadID = record.DownloadID
					found = true
					slog.InfoContext(ctx, "Found matching history record for blocklisting",
						"download_id", downloadID, "source_title", record.SourceTitle, "match_type", "fileID/title")
					break
				}
			}

			if found {
				break
			}
		}

		if downloadID == "" {
			slog.WarnContext(ctx, "Could not find import event in Sonarr history for file after checking multiple pages", "series_id", seriesID, "file_id", fileID, "release_title", releaseTitle)
			return nil
		}
	}

	// 2. Find the original grab event using the downloadId
	for page := 1; page <= maxPages; page++ {
		req := &starr.PageReq{
			PageSize: pageSize,
			Page:     page,
			SortKey:  "date",
			SortDir:  starr.SortDescend,
		}
		req.Set("seriesId", strconv.FormatInt(seriesID, 10))

		history, err := client.GetHistoryPageContext(ctx, req)
		if err != nil {
			return fmt.Errorf("failed to fetch Sonarr history page %d: %w", page, err)
		}

		if len(history.Records) == 0 {
			break
		}

		found := false
		for _, record := range history.Records {
			if record.DownloadID == downloadID && record.EventType == "grabbed" {
				slog.InfoContext(ctx, "Found grabbed history record, marking as failed to blocklist release",
					"history_id", record.ID, "download_id", downloadID, "page", page, "reason", reason)
				if failErr := client.FailContext(ctx, record.ID); failErr != nil {
					return fmt.Errorf("failed to fail Sonarr grab event %d: %w", record.ID, failErr)
				}
				return nil
			}
		}

		if found {
			return nil
		}
	}

	slog.WarnContext(ctx, "Could not find grab event in Sonarr history for download after checking multiple pages", "download_id", downloadID)
	return nil
}
