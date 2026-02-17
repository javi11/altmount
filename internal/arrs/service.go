package arrs

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/data"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/arrs/registrar"
	"github.com/javi11/altmount/internal/arrs/scanner"
	"github.com/javi11/altmount/internal/arrs/worker"
	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"golift.io/starr"
)

// Re-export types for backward compatibility
type ConfigInstance = model.ConfigInstance
type ConfigManager = model.ConfigManager

var (
	ErrPathMatchFailed         = model.ErrPathMatchFailed
	ErrEpisodeAlreadySatisfied = model.ErrEpisodeAlreadySatisfied
	ErrInstanceNotFound        = model.ErrInstanceNotFound
)

// Service manages Radarr and Sonarr instances for health monitoring and file repair
type Service struct {
	configGetter  config.ConfigGetter
	configManager model.ConfigManager
	userRepo      *database.UserRepository

	instances *instances.Manager
	clients   *clients.Manager
	data      *data.Manager
	scanner   *scanner.Manager
	worker    *worker.Worker
	registrar *registrar.Manager
}

// NewService creates a new arrs service for health monitoring and file repair
func NewService(configGetter config.ConfigGetter, configManager model.ConfigManager, userRepo *database.UserRepository) *Service {
	instManager := instances.NewManager(configGetter, configManager)
	clientManager := clients.NewManager()
	dataManager := data.NewManager()
	scannerManager := scanner.NewManager(configGetter, instManager, clientManager, dataManager)
	workerManager := worker.NewWorker(configGetter, instManager, clientManager)
	registrarManager := registrar.NewManager(instManager, clientManager)

	return &Service{
		configGetter:  configGetter,
		configManager: configManager,
		userRepo:      userRepo,
		instances:     instManager,
		clients:       clientManager,
		data:          dataManager,
		scanner:       scannerManager,
		worker:        workerManager,
		registrar:     registrarManager,
	}
}

// GetAllInstances returns all arrs instances from configuration
func (s *Service) GetAllInstances() []*model.ConfigInstance {
	return s.instances.GetAllInstances()
}

// GetInstance returns a specific instance by type and name
func (s *Service) GetInstance(instanceType, instanceName string) *model.ConfigInstance {
	return s.instances.GetInstance(instanceType, instanceName)
}

// RegisterInstance attempts to automatically register an ARR instance
func (s *Service) RegisterInstance(ctx context.Context, arrURL, apiKey string) error {
	registered, err := s.instances.RegisterInstance(ctx, arrURL, apiKey)
	if err != nil {
		return err
	}

	if registered {
		// Automatically register webhook for the new instance
		go func() {
			// Small delay to ensure DB/Config is fully settled
			time.Sleep(2 * time.Second)
			bgCtx := context.Background()

			key := s.GetFirstAdminAPIKey(bgCtx)
			if key != "" {
				// Use default internal URL
				_ = s.registrar.EnsureWebhookRegistration(bgCtx, "http://altmount:8080", key)
			}
		}()
	}

	return nil
}

// GetFirstAdminAPIKey retrieves the API key of the first admin user
func (s *Service) GetFirstAdminAPIKey(ctx context.Context) string {
	if s.userRepo == nil {
		return ""
	}
	users, err := s.userRepo.ListUsers(ctx, 100, 0)

	// If no users exist and auth is disabled, bootstrap a default admin
	if (err == nil && len(users) == 0) || (err != nil && strings.Contains(err.Error(), "no such table")) {
		cfg := s.configGetter()
		loginRequired := true
		if cfg.Auth.LoginRequired != nil {
			loginRequired = *cfg.Auth.LoginRequired
		}

		if !loginRequired {
			slog.InfoContext(ctx, "Bootstrapping default admin user for automatic background tasks")
			user := &database.User{
				UserID:   "admin",
				Provider: "direct",
				IsAdmin:  true,
			}
			if err := s.userRepo.CreateUser(ctx, user); err == nil {
				if key, err := s.userRepo.RegenerateAPIKey(ctx, user.UserID); err == nil {
					return key
				}
			}
		}
	}

	if err != nil || len(users) == 0 {
		return ""
	}

	for _, user := range users {
		if user.IsAdmin && user.APIKey != nil {
			return *user.APIKey
		}
	}

	// Fallback: if we have an admin but no key, generate one automatically
	for _, user := range users {
		if user.IsAdmin {
			if key, err := s.userRepo.RegenerateAPIKey(ctx, user.UserID); err == nil {
				return key
			}
		}
	}

	// Fallback to first user with a key
	if len(users) > 0 && users[0].APIKey != nil {
		return *users[0].APIKey
	}

	return ""
}

// StartWorker starts the queue cleanup worker
func (s *Service) StartWorker(ctx context.Context) error {
	return s.worker.Start(ctx)
}

// StopWorker stops the queue cleanup worker
func (s *Service) StopWorker(ctx context.Context) {
	s.worker.Stop(ctx)
}

// CleanupQueue checks all ARR instances for importPending items with empty folders
func (s *Service) CleanupQueue(ctx context.Context) error {
	return s.worker.CleanupQueue(ctx)
}

// TriggerFileRescan triggers a rescan for a specific file path through the appropriate ARR instance
func (s *Service) TriggerFileRescan(ctx context.Context, pathForRescan string, relativePath string) error {
	return s.scanner.TriggerFileRescan(ctx, pathForRescan, relativePath)
}

// TriggerScanForFile finds the ARR instance managing the file and triggers a download scan on it.
func (s *Service) TriggerScanForFile(ctx context.Context, filePath string) error {
	return s.scanner.TriggerScanForFile(ctx, filePath)
}

// TriggerDownloadScan triggers the "Check For Finished Downloads" task in ARR instances
func (s *Service) TriggerDownloadScan(ctx context.Context, instanceType string) {
	s.scanner.TriggerDownloadScan(ctx, instanceType)
}

// EnsureWebhookRegistration ensures that the AltMount webhook is registered in all enabled ARR instances
func (s *Service) EnsureWebhookRegistration(ctx context.Context, altmountURL string, apiKey string) error {
	return s.registrar.EnsureWebhookRegistration(ctx, altmountURL, apiKey)
}

// EnsureDownloadClientRegistration ensures that AltMount is registered as a SABnzbd download client in all enabled ARR instances
func (s *Service) EnsureDownloadClientRegistration(ctx context.Context, altmountHost string, altmountPort int, urlBase string, apiKey string) error {
	return s.registrar.EnsureDownloadClientRegistration(ctx, altmountHost, altmountPort, urlBase, apiKey)
}

// TestDownloadClientRegistration tests the connection from ARR instances back to AltMount
func (s *Service) TestDownloadClientRegistration(ctx context.Context, altmountHost string, altmountPort int, urlBase string, apiKey string) (map[string]string, error) {
	return s.registrar.TestDownloadClientRegistration(ctx, altmountHost, altmountPort, urlBase, apiKey)
}

// TestConnection tests the connection to an arrs instance
func (s *Service) TestConnection(ctx context.Context, instanceType, url, apiKey string) error {
	return s.clients.TestConnection(ctx, instanceType, url, apiKey)
}

// GetHealth retrieves health checks from all enabled ARR instances
func (s *Service) GetHealth(ctx context.Context) (map[string]any, error) {
	instances := s.instances.GetAllInstances()
	results := make(map[string]any)

	for _, instance := range instances {
		if !instance.Enabled {
			continue
		}

		var health any

		switch instance.Type {
		case "radarr":
			client, err := s.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Radarr client for health check", "instance", instance.Name, "error", err)
				continue
			}
			err = client.GetInto(ctx, starr.Request{URI: "/health"}, &health)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Radarr health", "instance", instance.Name, "error", err)
				continue
			}
			results[instance.Name] = health

		case "sonarr":
			client, err := s.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Sonarr client for health check", "instance", instance.Name, "error", err)
				continue
			}
			err = client.GetInto(ctx, starr.Request{URI: "/health"}, &health)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Sonarr health", "instance", instance.Name, "error", err)
				continue
			}
			results[instance.Name] = health
		}
	}

	return results, nil
}
