package rclonecli

//go:generate mockgen -source=./rclone_cli.go -destination=./rclone_cli_mock.go -package=rclonecli RcloneRcClient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

	baseUrl := c.config.VFSUrl
	if c.config.VFSUser != "" && c.config.VFSPass != "" {
		baseUrl = fmt.Sprintf("http://%s:%s@%s", c.config.VFSUser, c.config.VFSPass, c.config.VFSUrl)
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
