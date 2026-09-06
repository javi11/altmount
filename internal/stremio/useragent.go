package stremio

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	DefaultSonarrUserAgent = "Sonarr/4.1.1.824 (alpine 3.23.3)"
	DefaultRadarrUserAgent = "Radarr/6.5.1.2032 (alpine 3.23.3)"
)

// UserAgentInfo holds the current active user agents and detection metadata.
type UserAgentInfo struct {
	TVUserAgent     string    `json:"tv_user_agent"`
	MovieUserAgent  string    `json:"movie_user_agent"`
	SonarrVersion   string    `json:"sonarr_version"`
	RadarrVersion   string    `json:"radarr_version"`
	LastUpdated     time.Time `json:"last_updated"`
	Source          string    `json:"source"` // "local_instance", "github_release", "builtin_default"
}

// UserAgentManager manages dynamic detection and caching of ARR User-Agents.
type UserAgentManager struct {
	mu            sync.RWMutex
	httpClient    *http.Client
	info          UserAgentInfo
}

var (
	defaultManager *UserAgentManager
	once           sync.Once
)

// GetUserAgentManager returns the singleton UserAgentManager instance.
func GetUserAgentManager() *UserAgentManager {
	once.Do(func() {
		defaultManager = NewUserAgentManager(nil)
	})
	return defaultManager
}

// NewUserAgentManager creates a new UserAgentManager.
func NewUserAgentManager(httpClient *http.Client) *UserAgentManager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 6 * time.Second}
	}
	return &UserAgentManager{
		httpClient: httpClient,
		info: UserAgentInfo{
			TVUserAgent:    DefaultSonarrUserAgent,
			MovieUserAgent: DefaultRadarrUserAgent,
			SonarrVersion:  "4.1.1.824",
			RadarrVersion:  "6.5.1.2032",
			LastUpdated:    time.Now(),
			Source:         "builtin_default",
		},
	}
}

// GetUserAgent returns the appropriate User-Agent based on media type and configuration.
func (m *UserAgentManager) GetUserAgent(mediaType string, customOverride string) string {
	if customOverride != "" {
		return customOverride
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if strings.EqualFold(mediaType, "movie") {
		if m.info.MovieUserAgent != "" {
			return m.info.MovieUserAgent
		}
		return DefaultRadarrUserAgent
	}

	if m.info.TVUserAgent != "" {
		return m.info.TVUserAgent
	}
	return DefaultSonarrUserAgent
}

// GetInfo returns the current active User-Agent information.
func (m *UserAgentManager) GetInfo() UserAgentInfo {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.info
}

type githubReleaseResp struct {
	TagName string `json:"tag_name"`
	Name    string `json:"name"`
}

type arrSystemStatusResp struct {
	AppName   string `json:"appName"`
	Version   string `json:"version"`
	OsName    string `json:"osName"`
	OsVersion string `json:"osVersion"`
}

// CheckLocalARRs attempts to fetch live status from local Sonarr / Radarr instances.
func (m *UserAgentManager) CheckLocalARRs(ctx context.Context, sonarrURL, sonarrKey, radarrURL, radarrKey string) bool {
	updated := false

	if sonarrURL != "" && sonarrKey != "" {
		if status, err := m.fetchARRStatus(ctx, sonarrURL, sonarrKey); err == nil && status.Version != "" {
			osName := status.OsName
			if osName == "" {
				osName = "linux"
			}
			osVer := status.OsVersion
			if osVer == "" {
				osVer = "6.6"
			}
			m.mu.Lock()
			m.info.SonarrVersion = status.Version
			m.info.TVUserAgent = fmt.Sprintf("Sonarr/%s (%s %s)", status.Version, osName, osVer)
			m.info.LastUpdated = time.Now()
			m.info.Source = "local_instance"
			m.mu.Unlock()
			updated = true
		}
	}

	if radarrURL != "" && radarrKey != "" {
		if status, err := m.fetchARRStatus(ctx, radarrURL, radarrKey); err == nil && status.Version != "" {
			osName := status.OsName
			if osName == "" {
				osName = "linux"
			}
			osVer := status.OsVersion
			if osVer == "" {
				osVer = "6.6"
			}
			m.mu.Lock()
			m.info.RadarrVersion = status.Version
			m.info.MovieUserAgent = fmt.Sprintf("Radarr/%s (%s %s)", status.Version, osName, osVer)
			m.info.LastUpdated = time.Now()
			m.info.Source = "local_instance"
			m.mu.Unlock()
			updated = true
		}
	}

	return updated
}

// FetchLatestFromGitHub queries GitHub releases for latest Sonarr & Radarr version tags.
// userAgent is the configured User-Agent to send; empty falls back to a static identifier.
func (m *UserAgentManager) FetchLatestFromGitHub(ctx context.Context, userAgent string) error {
	sonarrVer, sonarrErr := m.fetchGitHubReleaseTag(ctx, "Sonarr/Sonarr", userAgent)
	radarrVer, radarrErr := m.fetchGitHubReleaseTag(ctx, "Radarr/Radarr", userAgent)

	if sonarrErr != nil && radarrErr != nil {
		return fmt.Errorf("failed to fetch from github: sonarr: %v; radarr: %v", sonarrErr, radarrErr)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if sonarrVer != "" {
		cleanVer := strings.TrimPrefix(sonarrVer, "v")
		m.info.SonarrVersion = cleanVer
		m.info.TVUserAgent = fmt.Sprintf("Sonarr/%s (alpine 3.23.3)", cleanVer)
	}
	if radarrVer != "" {
		cleanVer := strings.TrimPrefix(radarrVer, "v")
		m.info.RadarrVersion = cleanVer
		m.info.MovieUserAgent = fmt.Sprintf("Radarr/%s (alpine 3.23.3)", cleanVer)
	}
	m.info.LastUpdated = time.Now()
	m.info.Source = "github_release"
	return nil
}

func (m *UserAgentManager) fetchGitHubReleaseTag(ctx context.Context, repo, userAgent string) (string, error) {
	apiURL := fmt.Sprintf("https://api.github.com/repos/%s/releases/latest", repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiURL, nil)
	if err != nil {
		return "", err
	}
	if userAgent == "" {
		userAgent = "AltMount/1.0"
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github api returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var rel githubReleaseResp
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", err
	}

	tag := rel.TagName
	if tag == "" {
		tag = rel.Name
	}
	return tag, nil
}

func (m *UserAgentManager) fetchARRStatus(ctx context.Context, baseURL, apiKey string) (*arrSystemStatusResp, error) {
	statusURL := fmt.Sprintf("%s/api/v3/system/status", strings.TrimRight(baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, statusURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-Api-Key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("system status returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var status arrSystemStatusResp
	if err := json.Unmarshal(body, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
