package instances

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"strings"

	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

type Manager struct {
	configGetter  config.ConfigGetter
	configManager model.ConfigManager
}

func NewManager(configGetter config.ConfigGetter, configManager model.ConfigManager) *Manager {
	return &Manager{
		configGetter:  configGetter,
		configManager: configManager,
	}
}

// GetAllInstances returns all arrs instances from current configuration
func (m *Manager) GetAllInstances() []*model.ConfigInstance {
	cfg := m.configGetter()
	instances := make([]*model.ConfigInstance, 0)

	// Convert Radarr instances
	if len(cfg.Arrs.RadarrInstances) > 0 {
		for _, radarrConfig := range cfg.Arrs.RadarrInstances {
			instance := &model.ConfigInstance{
				Name:     radarrConfig.Name,
				Type:     "radarr",
				URL:      radarrConfig.URL,
				APIKey:   radarrConfig.APIKey,
				Category: radarrConfig.Category,
				Enabled:  radarrConfig.Enabled != nil && *radarrConfig.Enabled,
			}
			instances = append(instances, instance)
		}
	}

	// Convert Sonarr instances
	if len(cfg.Arrs.SonarrInstances) > 0 {
		for _, sonarrConfig := range cfg.Arrs.SonarrInstances {
			instance := &model.ConfigInstance{
				Name:     sonarrConfig.Name,
				Type:     "sonarr",
				URL:      sonarrConfig.URL,
				APIKey:   sonarrConfig.APIKey,
				Category: sonarrConfig.Category,
				Enabled:  sonarrConfig.Enabled != nil && *sonarrConfig.Enabled,
			}
			instances = append(instances, instance)
		}
	}

	return instances
}

// FindConfigInstance finds a specific instance by type and name
func (m *Manager) FindConfigInstance(instanceType, instanceName string) (*model.ConfigInstance, error) {
	instances := m.GetAllInstances()
	for _, instance := range instances {
		if instance.Type == instanceType && instance.Name == instanceName {
			return instance, nil
		}
	}

	return nil, fmt.Errorf("instance not found: %s/%s", instanceType, instanceName)
}

// GetInstance returns a specific instance by type and name (nil if not found)
func (m *Manager) GetInstance(instanceType, instanceName string) *model.ConfigInstance {
	inst, err := m.FindConfigInstance(instanceType, instanceName)
	if err != nil {
		return nil
	}
	return inst
}

// RegisterInstance attempts to automatically register an ARR instance
// It returns true if a new instance was registered, false if it already existed
func (m *Manager) RegisterInstance(ctx context.Context, arrURL, apiKey string) (bool, error) {
	if m.configManager == nil {
		return false, fmt.Errorf("config manager not available")
	}

	slog.InfoContext(ctx, "Attempting to register ARR instance", "url", arrURL)

	// Check if instance already exists
	if m.instanceExistsByURL(arrURL) {
		slog.DebugContext(ctx, "ARR instance already exists, skipping registration", "url", arrURL)
		return false, nil
	}

	// Detect ARR type
	arrType, err := m.detectARRType(ctx, arrURL, apiKey)
	if err != nil {
		return false, fmt.Errorf("failed to detect ARR type: %w", err)
	}

	// Determine category based on ARR type
	var category string
	switch arrType {
	case "radarr":
		category = "movies"
	case "sonarr":
		category = "tv"
	default:
		return false, fmt.Errorf("unsupported ARR type: %s", arrType)
	}

	// Generate instance name
	instanceName, err := m.generateInstanceName(arrURL)
	if err != nil {
		return false, fmt.Errorf("failed to generate instance name: %w", err)
	}

	// If default category is already used by another instance, generate a unique one
	if m.categoryUsedByOtherInstance(arrType, category) {
		category = fmt.Sprintf("%s-%s", category, instanceName)
	}

	slog.InfoContext(ctx, "Registering new ARR instance",
		"name", instanceName,
		"type", arrType,
		"url", arrURL,
		"category", category)

	// Get current config and make a deep copy
	currentConfig := m.configManager.GetConfig()
	newConfig := currentConfig.DeepCopy()

	// Create new instance config
	enabled := true
	newInstance := config.ArrsInstanceConfig{
		Name:     instanceName,
		URL:      arrURL,
		APIKey:   apiKey,
		Category: category,
		Enabled:  &enabled,
	}

	// Add to appropriate array
	switch arrType {
	case "radarr":
		newConfig.Arrs.RadarrInstances = append(newConfig.Arrs.RadarrInstances, newInstance)
	case "sonarr":
		newConfig.Arrs.SonarrInstances = append(newConfig.Arrs.SonarrInstances, newInstance)
	}

	// Create category for this ARR type
	m.ensureCategoryExistsInConfig(ctx, newConfig, category)

	// Update and save configuration
	if err := m.configManager.UpdateConfig(newConfig); err != nil {
		return false, fmt.Errorf("failed to update configuration: %w", err)
	}

	if err := m.configManager.SaveConfig(); err != nil {
		return false, fmt.Errorf("failed to save configuration: %w", err)
	}

	slog.InfoContext(ctx, "Successfully registered ARR instance",
		"name", instanceName,
		"type", arrType,
		"url", arrURL,
		"category", category)

	return true, nil
}

// detectARRType attempts to detect if a URL points to Radarr or Sonarr
func (m *Manager) detectARRType(ctx context.Context, arrURL, apiKey string) (string, error) {
	slog.DebugContext(ctx, "Detecting ARR type", "url", arrURL)

	// Try Radarr first
	radarrClient := radarr.New(&starr.Config{URL: arrURL, APIKey: apiKey})
	radarrStatus, err := radarrClient.GetSystemStatusContext(ctx)
	if err == nil {
		switch radarrStatus.AppName {
		case "Radarr":
			slog.DebugContext(ctx, "Detected Radarr instance", "url", arrURL)
			return "radarr", nil
		case "Sonarr":
			slog.DebugContext(ctx, "Detected Sonarr instance", "url", arrURL)
			return "sonarr", nil
		default:
			slog.DebugContext(ctx, "Unknown AppName from Radarr client", "app_name", radarrStatus.AppName, "url", arrURL)
		}
	}

	// Try Sonarr
	sonarrClient := sonarr.New(&starr.Config{URL: arrURL, APIKey: apiKey})
	sonarrStatus, err := sonarrClient.GetSystemStatusContext(ctx)
	if err == nil {
		switch sonarrStatus.AppName {
		case "Radarr":
			slog.DebugContext(ctx, "Detected Radarr instance", "url", arrURL)
			return "radarr", nil
		case "Sonarr":
			slog.DebugContext(ctx, "Detected Sonarr instance", "url", arrURL)
			return "sonarr", nil
		default:
			slog.DebugContext(ctx, "Unknown AppName from Sonarr client", "app_name", sonarrStatus.AppName, "url", arrURL)
		}
	}

	return "", fmt.Errorf("unable to detect ARR type for URL %s - neither Radarr nor Sonarr responded successfully", arrURL)
}

// generateInstanceName generates an instance name from a URL
func (m *Manager) generateInstanceName(rawURL string) (string, error) {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	hostname := parsedURL.Hostname()
	port := parsedURL.Port()

	if port == "" {
		if parsedURL.Scheme == "https" {
			port = "443"
		} else {
			port = "80"
		}
	}

	return fmt.Sprintf("%s-%s", hostname, port), nil
}

// instanceExistsByURL checks if an instance with the given URL already exists
func (m *Manager) instanceExistsByURL(checkURL string) bool {
	normalizedCheck := normalizeURL(checkURL)
	instances := m.GetAllInstances()

	for _, instance := range instances {
		normalizedInstance := normalizeURL(instance.URL)
		if normalizedInstance == normalizedCheck {
			return true
		}
	}

	return false
}

func normalizeURL(rawURL string) string {
	return strings.TrimSuffix(rawURL, "/")
}

// categoryUsedByOtherInstance checks if a category is already used by another instance of the same type
func (m *Manager) categoryUsedByOtherInstance(arrType, category string) bool {
	var instances []config.ArrsInstanceConfig
	cfg := m.configManager.GetConfig()

	switch arrType {
	case "radarr":
		instances = cfg.Arrs.RadarrInstances
	case "sonarr":
		instances = cfg.Arrs.SonarrInstances
	}

	for _, instance := range instances {
		instanceCat := instance.Category
		if instanceCat == "" {
			if arrType == "radarr" {
				instanceCat = "movies"
			} else {
				instanceCat = "tv"
			}
		}

		if instanceCat == category {
			return true
		}
	}

	return false
}

// ensureCategoryExistsInConfig ensures a category exists in the provided config
func (m *Manager) ensureCategoryExistsInConfig(ctx context.Context, cfg *config.Config, category string) {
	if category == "" {
		category = "default"
	}

	for _, existingCategory := range cfg.SABnzbd.Categories {
		if existingCategory.Name == category {
			slog.DebugContext(ctx, "Category already exists, skipping creation", "category", category)
			return
		}
	}

	nextOrder := 0
	for _, existingCategory := range cfg.SABnzbd.Categories {
		if existingCategory.Order >= nextOrder {
			nextOrder = existingCategory.Order + 1
		}
	}

	newCategory := config.SABnzbdCategory{
		Name:     category,
		Order:    nextOrder,
		Priority: 0,
		Dir:      category,
	}

	cfg.SABnzbd.Categories = append(cfg.SABnzbd.Categories, newCategory)

	slog.InfoContext(ctx, "Created new category",
		"category", category,
		"order", nextOrder,
		"dir", category)
}
