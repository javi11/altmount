package rclonecli

//go:generate mockgen -source=./rclone_cli.go -destination=./rclone_cli_mock.go -package=rclonecli RcloneRcClient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type RcloneRcClient interface {
	RefreshCache(ctx context.Context, dir string, async, recursive bool) error
}

type rcloneRcClient struct {
	config     *Config
	httpClient *http.Client
}

func NewRcloneRcClient(
	config *Config,
	httpClient *http.Client,
) RcloneRcClient {
	return &rcloneRcClient{
		config:     config,
		httpClient: httpClient,
	}
}

func (c *rcloneRcClient) RefreshCache(ctx context.Context, dir string, async, recursive bool) error {
	// Check if VFS notifications are enabled
	if !c.config.VFSEnabled {
		return nil // Silently skip if VFS is not enabled
	}

	// Check if VFS URL is configured
	if c.config.VFSUrl == "" {
		return fmt.Errorf("VFS URL is not configured")
	}

	data := map[string]string{
		"_async":    fmt.Sprintf("%t", async),
		"recursive": fmt.Sprintf("%t", recursive),
	}

	baseUrl, err := c.buildVFSUrl()
	if err != nil {
		return fmt.Errorf("invalid VFS URL configuration: %w", err)
	}

	if dir != "" {
		data["dir"] = dir
	}

	payload, err := json.Marshal(data)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", baseUrl+"/vfs/refresh", bytes.NewBuffer(payload))
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

	return nil
}

// buildVFSUrl constructs the VFS URL with proper protocol and authentication handling
func (c *rcloneRcClient) buildVFSUrl() (string, error) {
	rawUrl := c.config.VFSUrl
	if rawUrl == "" {
		return "", fmt.Errorf("VFS URL is not configured")
	}

	// Parse the URL to handle all cases properly
	parsedUrl, err := url.Parse(rawUrl)
	if err != nil {
		// If parsing fails, return the error immediately
		return "", fmt.Errorf("failed to parse VFS URL %q: %w", c.config.VFSUrl, err)
	}

	// If no scheme is present, or if it looks like hostname:port was parsed as scheme:opaque
	// (which happens with URLs like "example.com:8080"), add http:// and re-parse
	needsScheme := parsedUrl.Scheme == "" ||
		(parsedUrl.Host == "" && parsedUrl.Opaque != "" &&
		 parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https")

	if needsScheme {
		rawUrl = "http://" + c.config.VFSUrl
		parsedUrl, err = url.Parse(rawUrl)
		if err != nil {
			return "", fmt.Errorf("failed to parse VFS URL %q after adding http prefix: %w", c.config.VFSUrl, err)
		}
	}

	// Validate scheme
	if parsedUrl.Scheme != "http" && parsedUrl.Scheme != "https" {
		return "", fmt.Errorf("unsupported URL scheme %q, only http and https are supported", parsedUrl.Scheme)
	}

	// Handle authentication
	if c.config.VFSUser != "" && c.config.VFSPass != "" {
		// Set authentication, this will override any existing userinfo
		parsedUrl.User = url.UserPassword(c.config.VFSUser, c.config.VFSPass)
	}

	// Ensure host is present
	if parsedUrl.Host == "" {
		return "", fmt.Errorf("VFS URL must contain a valid host")
	}

	return parsedUrl.String(), nil
}
