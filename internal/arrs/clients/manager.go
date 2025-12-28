package clients

import (
	"context"
	"fmt"
	"sync"

	"github.com/javi11/altmount/internal/arrs/model"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

type Manager struct {
	mu            sync.RWMutex
	radarrClients map[string]*radarr.Radarr // key: instance name
	sonarrClients map[string]*sonarr.Sonarr // key: instance name
}

func NewManager() *Manager {
	return &Manager{
		radarrClients: make(map[string]*radarr.Radarr),
		sonarrClients: make(map[string]*sonarr.Sonarr),
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

// GetOrCreateClient is a helper to get or create the appropriate client
func (m *Manager) GetOrCreateClient(instance *model.ConfigInstance) (interface{}, error) {
	if instance.Type == "radarr" {
		return m.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
	}
	return m.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
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

	default:
		return fmt.Errorf("unsupported instance type: %s", instanceType)
	}
}
