package rclonecli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/javi11/altmount/internal/config"
)

type RcloneRcClient interface {
	RefreshDir(ctx context.Context, provider string, dirs []string) error
}

type rcloneRcClient struct {
	config     *config.Manager
	httpClient *http.Client
}

func NewRcloneRcClient(
	config *config.Manager,
	httpClient *http.Client,
) RcloneRcClient {
	return &rcloneRcClient{
		config:     config,
		httpClient: httpClient,
	}
}

func TestConnection(
	ctx context.Context,
	rcUrl string,
	rcUser string,
	rcPass string,
	httpClient *http.Client,
) error {
	if rcUrl == "" {
		return fmt.Errorf("RC URL is not configured")
	}

	baseUrl, err := buildRCUrl(rcUrl, rcUser, rcPass)
	if err != nil {
		return fmt.Errorf("invalid RC URL configuration: %w", err)
	}

	data := map[string]string{}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseUrl+"/rc/noop", bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	return nil
}

func (c *rcloneRcClient) RefreshDir(ctx context.Context, provider string, dirs []string) error {
	cfg := c.config.GetConfig()

	// Check if RC notifications are enabled
	if cfg.RClone.RCEnabled == nil || !*cfg.RClone.RCEnabled {
		return nil // Silently skip if RC is not enabled
	}

	// Check if RC URL is configured
	if cfg.RClone.RCUrl == "" {
		return fmt.Errorf("RC URL is not configured")
	}

	// If no specific directories provided, refresh root
	if len(dirs) == 0 {
		dirs = []string{"/"}
	}

	baseUrl, err := buildRCUrl(cfg.RClone.RCUrl, cfg.RClone.RCUser, cfg.RClone.RCPass)
	if err != nil {
		return fmt.Errorf("invalid RC URL configuration: %w", err)
	}

	// Use similar logic to Manager's RefreshDir but with vfs/refresh endpoint
	refreshArgs := map[string]interface{}{
		"_async":    "false", // Use async refresh
		"recursive": "false", // Non-recursive by default
	}

	// Add filesystem specification if provider is provided
	if provider != "" {
		refreshArgs["fs"] = fmt.Sprintf("%s:", provider)
	}

	// Add directories to refresh
	for i, dir := range dirs {
		if dir != "" {
			if i == 0 {
				refreshArgs["dir"] = dir
			} else {
				refreshArgs[fmt.Sprintf("dir%d", i+1)] = dir
			}
		}
	}

	forgetArgs := make(map[string]interface{})
	for k, v := range refreshArgs {
		if k != "recursive" {
			forgetArgs[k] = v
		}
	}

	forgetPayload, err := json.Marshal(forgetArgs)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", baseUrl+"/vfs/forget", bytes.NewBuffer(forgetPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected status code: %d, error: %s", resp.StatusCode, string(body))
	}

	refreshPayload, err := json.Marshal(refreshArgs)
	if err != nil {
		return err
	}

	req, err = http.NewRequestWithContext(ctx, "POST", baseUrl+"/vfs/refresh", bytes.NewBuffer(refreshPayload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err = c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		return fmt.Errorf("unexpected status code: %d, error: %s", resp.StatusCode, string(body))
	}

	return nil
}

// buildRCUrl constructs the RC URL with proper protocol and authentication handling
func buildRCUrl(
	rcUrl string,
	rcUser string,
	rcPass string,
) (string, error) {
	rawUrl := rcUrl
	if rawUrl == "" {
		return "", fmt.Errorf("RC URL is not configured")
	}

	// Parse the URL to handle all cases properly
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		// If parsing fails, return the error immediately
		return "", fmt.Errorf("failed to parse RC URL %q: %w", rcUrl, err)
	}

	// If no scheme is present, or if it looks like hostname:port was parsed as scheme:opaque
	// (which happens with URLs like "example.com:8080"), add http:// and re-parse
	needsScheme := parsedUrl.Scheme == "" ||
		(parsedUrl.Host == "" && parsedUrl.Opaque != "" &&
			parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https")

	if needsScheme {
		rawUrl = "http://" + rcUrl
		parsedUrl, err = url.Parse(rawUrl)
		if err != nil {
			return "", fmt.Errorf("failed to parse RC URL %q after adding http prefix: %w", rcUrl, err)
		}
	}

	// Validate scheme
	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return "", fmt.Errorf("unsupported RC URL scheme %q, only http and https are supported", parsedUrl.Scheme)
	}

	// Handle authentication
	if rcUser != "" && rcPass != "" {
		// Set authentication, this will override any existing userinfo
		parsedUrl.User = url.UserPassword(rcUser, rcPass)
	}

	// Ensure host is present
	if parsedUrl.Host == "" {
		return "", fmt.Errorf("RC URL must contain a valid host")
	}

	return parsedUrl.String(), nil
}
