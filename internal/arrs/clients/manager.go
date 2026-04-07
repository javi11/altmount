package clients

import (
	"context"
	"fmt"
	"sync"

	"github.com/javi11/altmount/internal/arrs/model"
	"golift.io/starr"
	"golift.io/starr/lidarr"
	"golift.io/starr/radarr"
	"golift.io/starr/readarr"
	"golift.io/starr/sonarr"
	)

type Manager struct {
	mu              sync.RWMutex
	radarrClients   map[string]*radarr.Radarr     // key: instance name
	sonarrClients   map[string]*sonarr.Sonarr     // key: instance name
	lidarrClients   map[string]*lidarr.Lidarr     // key: instance name
	readarrClients  map[string]*readarr.Readarr   // key: instance name
	whisparrClients map[string]*radarr.Radarr // key: instance name
}

func NewManager() *Manager {
	return &Manager{
		radarrClients:   make(map[string]*radarr.Radarr),
		sonarrClients:   make(map[string]*sonarr.Sonarr),
		lidarrClients:   make(map[string]*lidarr.Lidarr),
		readarrClients:  make(map[string]*readarr.Readarr),
		whisparrClients: make(map[string]*radarr.Radarr),
	}
}

// GetOrCreateRadarrClient gets or creates a Radarr client for an instance
func (m *Manager) GetOrCreateRadarrClient(instanceName, url, apiKey string) (*radarr.Radarr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.radarrClients[instanceName]; exists {
		return client, nil
	}

	client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
	m.radarrClients[instanceName] = client
	return client, nil
}

// GetOrCreateSonarrClient gets or creates a Sonarr client for an instance
func (m *Manager) GetOrCreateSonarrClient(instanceName, url, apiKey string) (*sonarr.Sonarr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.sonarrClients[instanceName]; exists {
		return client, nil
	}

	client := sonarr.New(&starr.Config{URL: url, APIKey: apiKey})
	m.sonarrClients[instanceName] = client
	return client, nil
}

// GetOrCreateLidarrClient gets or creates a Lidarr client for an instance
func (m *Manager) GetOrCreateLidarrClient(instanceName, url, apiKey string) (*lidarr.Lidarr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.lidarrClients[instanceName]; exists {
		return client, nil
	}

	client := lidarr.New(&starr.Config{URL: url, APIKey: apiKey})
	m.lidarrClients[instanceName] = client
	return client, nil
}

// GetOrCreateReadarrClient gets or creates a Readarr client for an instance
func (m *Manager) GetOrCreateReadarrClient(instanceName, url, apiKey string) (*readarr.Readarr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.readarrClients[instanceName]; exists {
		return client, nil
	}

	client := readarr.New(&starr.Config{URL: url, APIKey: apiKey})
	m.readarrClients[instanceName] = client
	return client, nil
}

// GetOrCreateWhisparrClient gets or creates a Whisparr client for an instance (using Radarr client)
func (m *Manager) GetOrCreateWhisparrClient(instanceName, url, apiKey string) (*radarr.Radarr, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if client, exists := m.whisparrClients[instanceName]; exists {
		return client, nil
	}

	client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
	m.whisparrClients[instanceName] = client
	return client, nil
}

// GetOrCreateClient is a helper to get or create the appropriate client
func (m *Manager) GetOrCreateClient(instance *model.ConfigInstance) (any, error) {
	switch instance.Type {
	case "radarr":
		return m.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
	case "sonarr":
		return m.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
	case "lidarr":
		return m.GetOrCreateLidarrClient(instance.Name, instance.URL, instance.APIKey)
	case "readarr":
		return m.GetOrCreateReadarrClient(instance.Name, instance.URL, instance.APIKey)
	case "whisparr":
		return m.GetOrCreateWhisparrClient(instance.Name, instance.URL, instance.APIKey)
	default:
		return nil, fmt.Errorf("unsupported instance type: %s", instance.Type)
	}
}

// TestConnection tests the connection to an arrs instance
func (m *Manager) TestConnection(ctx context.Context, instanceType, url, apiKey string) error {
	switch instanceType {
	case "radarr":
		client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatusContext(ctx)
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

	case "lidarr":
		client := lidarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatusContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to Lidarr: %w", err)
		}
		return nil

	case "readarr":
		client := readarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatusContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to Readarr: %w", err)
		}
		return nil

	case "whisparr":
		client := radarr.New(&starr.Config{URL: url, APIKey: apiKey})
		_, err := client.GetSystemStatusContext(ctx)
		if err != nil {
			return fmt.Errorf("failed to connect to Whisparr: %w", err)
		}
		return nil

	default:
		return fmt.Errorf("unsupported instance type: %s", instanceType)
	}
}
