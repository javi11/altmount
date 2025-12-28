package registrar

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/instances"
	"golift.io/starr"
	"golift.io/starr/radarr"
	"golift.io/starr/sonarr"
)

type Manager struct {
	instances *instances.Manager
	clients   *clients.Manager
}

func NewManager(instances *instances.Manager, clients *clients.Manager) *Manager {
	return &Manager{
		instances: instances,
		clients:   clients,
	}
}

// EnsureWebhookRegistration ensures that the AltMount webhook is registered in all enabled ARR instances
func (m *Manager) EnsureWebhookRegistration(ctx context.Context, altmountURL string, apiKey string) error {
	allInstances := m.instances.GetAllInstances()
	webhookName := "AltMount Webhook"
	webhookURL := fmt.Sprintf("%s/api/arrs/webhook?apikey=%s", altmountURL, apiKey)

	slog.InfoContext(ctx, "Ensuring webhook registration in ARR instances", "webhook_url", webhookURL)

	for _, instance := range allInstances {
		if !instance.Enabled {
			continue
		}

		slog.DebugContext(ctx, "Checking webhook for instance", "instance", instance.Name, "type", instance.Type)

		switch instance.Type {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Radarr client for webhook check", "instance", instance.Name, "error", err)
				continue
			}

			notifications, err := client.GetNotificationsContext(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Radarr notifications", "instance", instance.Name, "error", err)
				continue
			}

			exists := false
			for _, n := range notifications {
				if n.Name == webhookName {
					exists = true
					// potentially update if needed, but for now just skip
					break
				}
			}

			if !exists {
				notif := &radarr.NotificationInput{
					Name:                        webhookName,
					Implementation:              "Webhook",
					ConfigContract:              "WebhookSettings",
					OnGrab:                      false,
					OnDownload:                  true, // OnImport
					OnUpgrade:                   true,
					OnRename:                    true,
					OnMovieDelete:               true,
					OnMovieFileDelete:           true,
					OnMovieFileDeleteForUpgrade: true,
					Fields: []*starr.FieldInput{
						{Name: "url", Value: webhookURL},
						{Name: "method", Value: "1"}, // 1 = POST
					},
				}
				_, err := client.AddNotificationContext(ctx, notif)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to add Radarr webhook", "instance", instance.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "Added AltMount webhook to Radarr", "instance", instance.Name)
				}
			}

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Sonarr client for webhook check", "instance", instance.Name, "error", err)
				continue
			}

			notifications, err := client.GetNotificationsContext(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Sonarr notifications", "instance", instance.Name, "error", err)
				continue
			}

			exists := false
			for _, n := range notifications {
				if n.Name == webhookName {
					exists = true
					break
				}
			}

			if !exists {
				notif := &sonarr.NotificationInput{
					Name:                          webhookName,
					Implementation:                "Webhook",
					ConfigContract:                "WebhookSettings",
					OnGrab:                        false,
					OnDownload:                    true, // OnImport
					OnUpgrade:                     true,
					OnRename:                      true,
					OnSeriesDelete:                true,
					OnEpisodeFileDelete:           true,
					OnEpisodeFileDeleteForUpgrade: true,
					Fields: []*starr.FieldInput{
						{Name: "url", Value: webhookURL},
						{Name: "method", Value: "1"}, // 1 = POST
					},
				}
				_, err := client.AddNotificationContext(ctx, notif)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to add Sonarr webhook", "instance", instance.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "Added AltMount webhook to Sonarr", "instance", instance.Name)
				}
			}
		}
	}

	return nil
}

// EnsureDownloadClientRegistration ensures that AltMount is registered as a SABnzbd download client in all enabled ARR instances
func (m *Manager) EnsureDownloadClientRegistration(ctx context.Context, altmountHost string, altmountPort int, urlBase string, apiKey string) error {
	allInstances := m.instances.GetAllInstances()
	clientName := "AltMount (SABnzbd)"

	slog.InfoContext(ctx, "Ensuring AltMount download client registration in ARR instances",
		"host", altmountHost,
		"port", altmountPort,
		"url_base", urlBase)

	for _, instance := range allInstances {
		if !instance.Enabled {
			continue
		}

		slog.DebugContext(ctx, "Checking download client for instance", "instance", instance.Name, "type", instance.Type)

		switch instance.Type {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Radarr client for download client check", "instance", instance.Name, "error", err)
				continue
			}

			clients, err := client.GetDownloadClientsContext(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Radarr download clients", "instance", instance.Name, "error", err)
				continue
			}

			exists := false
			for _, c := range clients {
				if c.Name == clientName {
					exists = true
					break
				}
			}

			if !exists {
				category := instance.Category
				if category == "" {
					category = "movies"
				}
				dc := &radarr.DownloadClientInput{
					Name:                     clientName,
					Implementation:           "SABnzbd",
					ConfigContract:           "SABnzbdSettings",
					Enable:                   true,
					RemoveCompletedDownloads: true,
					RemoveFailedDownloads:    true,
					Priority:                 1,
					Protocol:                 "Usenet",
					Fields: []*starr.FieldInput{
						{Name: "host", Value: altmountHost},
						{Name: "port", Value: altmountPort},
						{Name: "urlBase", Value: urlBase},
						{Name: "apiKey", Value: apiKey},
						{Name: "movieCategory", Value: category},
						{Name: "useSsl", Value: false},
					},
				}
				_, err := client.AddDownloadClientContext(ctx, dc)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to add Radarr download client", "instance", instance.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "Added AltMount download client to Radarr", "instance", instance.Name)
				}
			}

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to create Sonarr client for download client check", "instance", instance.Name, "error", err)
				continue
			}

			clients, err := client.GetDownloadClientsContext(ctx)
			if err != nil {
				slog.ErrorContext(ctx, "Failed to get Sonarr download clients", "instance", instance.Name, "error", err)
				continue
			}

			exists := false
			for _, c := range clients {
				if c.Name == clientName {
					exists = true
					break
				}
			}

			if !exists {
				category := instance.Category
				if category == "" {
					category = "tv"
				}
				dc := &sonarr.DownloadClientInput{
					Name:                     clientName,
					Implementation:           "SABnzbd",
					ConfigContract:           "SABnzbdSettings",
					Enable:                   true,
					RemoveCompletedDownloads: true,
					RemoveFailedDownloads:    true,
					Priority:                 1,
					Protocol:                 "Usenet",
					Fields: []*starr.FieldInput{
						{Name: "host", Value: altmountHost},
						{Name: "port", Value: altmountPort},
						{Name: "urlBase", Value: urlBase},
						{Name: "apiKey", Value: apiKey},
						{Name: "tvCategory", Value: category},
						{Name: "useSsl", Value: false},
					},
				}
				_, err := client.AddDownloadClientContext(ctx, dc)
				if err != nil {
					slog.ErrorContext(ctx, "Failed to add Sonarr download client", "instance", instance.Name, "error", err)
				} else {
					slog.InfoContext(ctx, "Added AltMount download client to Sonarr", "instance", instance.Name)
				}
			}
		}
	}

	return nil
}

// TestDownloadClientRegistration tests the connection from ARR instances back to AltMount
func (m *Manager) TestDownloadClientRegistration(ctx context.Context, altmountHost string, altmountPort int, urlBase string, apiKey string) (map[string]string, error) {
	allInstances := m.instances.GetAllInstances()
	results := make(map[string]string)

	for _, instance := range allInstances {
		if !instance.Enabled {
			continue
		}

		var testErr error
		switch instance.Type {
		case "radarr":
			client, err := m.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				results[instance.Name] = fmt.Sprintf("Failed to create client: %v", err)
				continue
			}

			dc := &radarr.DownloadClientInput{
				Name:           "AltMount Test",
				Implementation: "SABnzbd",
				ConfigContract: "SABnzbdSettings",
				Enable:         true,
				Priority:       1,
				Protocol:       "Usenet",
				Fields: []*starr.FieldInput{
					{Name: "host", Value: altmountHost},
					{Name: "port", Value: altmountPort},
					{Name: "urlBase", Value: urlBase},
					{Name: "apiKey", Value: apiKey},
					{Name: "movieCategory", Value: "movies"},
					{Name: "useSsl", Value: false},
				},
			}
			testErr = client.TestDownloadClientContext(ctx, dc)

		case "sonarr":
			client, err := m.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
			if err != nil {
				results[instance.Name] = fmt.Sprintf("Failed to create client: %v", err)
				continue
			}

			dc := &sonarr.DownloadClientInput{
				Name:           "AltMount Test",
				Implementation: "SABnzbd",
				ConfigContract: "SABnzbdSettings",
				Enable:         true,
				Priority:       1,
				Protocol:       "Usenet",
				Fields: []*starr.FieldInput{
					{Name: "host", Value: altmountHost},
					{Name: "port", Value: altmountPort},
					{Name: "urlBase", Value: urlBase},
					{Name: "apiKey", Value: apiKey},
					{Name: "tvCategory", Value: "tv"},
					{Name: "useSsl", Value: false},
				},
			}
			testErr = client.TestDownloadClientContext(ctx, dc)
		}

		if testErr != nil {
			results[instance.Name] = testErr.Error()
		} else {
			results[instance.Name] = "OK"
		}
	}

	return results, nil
}
