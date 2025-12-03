package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/spf13/cobra"
)

func init() {
	scanCmd := &cobra.Command{
		Use:   "scan",
		Short: "Trigger a library scan",
		Long:  `Trigger a manual library scan (library sync) on the running server.`,
		RunE:  runScan,
	}

	scanCmd.Flags().Bool("dry-run", false, "Perform a dry run scan")

	rootCmd.AddCommand(scanCmd)
}

func runScan(cmd *cobra.Command, args []string) error {
	// Load config to get port
	cfg, err := config.LoadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	port := cfg.WebDAV.Port
	prefix := cfg.API.Prefix

	// Determine which endpoint to call
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	endpoint := "start"
	if dryRun {
		endpoint = "dry-run"
	}

	url := fmt.Sprintf("http://localhost:%d%s/health/library-sync/%s", port, prefix, endpoint)
	
	slog.Info("Triggering library scan", "url", url, "dry_run", dryRun)

	// Create client with timeout
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	// Create request
	req, err := http.NewRequest("POST", url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// If auth is required, we might need to handle it. 
	// For now, assuming localhost access or no auth for internal CLI usage if configured that way.
	// However, the API usually requires auth if LoginRequired is true.
	// The CLI might need an API key or token. 
	// Checking if there's a way to bypass or provide credentials.
	// Since this is a "convenience" CLI command, let's see if we can get an admin token or similar.
	// If not, we'll warn the user they might need to authenticate if the request fails with 401.

	// Execute request
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("authentication failed: you may need to configure authentication or use an API key")
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned error: %s (body: %s)", resp.Status, string(body))
	}

	// Pretty print JSON response
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, body, "", "  "); err != nil {
		fmt.Println(string(body))
	} else {
		fmt.Println(prettyJSON.String())
	}

	return nil
}
