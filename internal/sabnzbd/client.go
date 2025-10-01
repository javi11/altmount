package sabnzbd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// SABnzbdClient handles communication with external SABnzbd instances
type SABnzbdClient struct {
	httpClient *http.Client
}

// NewSABnzbdClient creates a new SABnzbd client with reasonable timeouts
func NewSABnzbdClient() *SABnzbdClient {
	return &SABnzbdClient{
		httpClient: &http.Client{
			Timeout: 60 * time.Second, // 60 second timeout for file uploads
		},
	}
}

// SABnzbdAPIResponse represents a response from SABnzbd API
type SABnzbdAPIResponse struct {
	Status bool    `json:"status"`
	Error  *string `json:"error,omitempty"`
	NzoIds []string `json:"nzo_ids,omitempty"`
}

// SendNZBFile sends an NZB file to an external SABnzbd instance
// Returns the NZO ID assigned by SABnzbd, or an error
// Priority should be "0" (low), "1" (normal), or "2" (high)
func (c *SABnzbdClient) SendNZBFile(host, apiKey, nzbPath string, category *string, priority *string) (string, error) {
	// Validate inputs
	if host == "" {
		return "", fmt.Errorf("SABnzbd host cannot be empty")
	}
	if apiKey == "" {
		return "", fmt.Errorf("SABnzbd API key cannot be empty")
	}
	if nzbPath == "" {
		return "", fmt.Errorf("NZB file path cannot be empty")
	}

	// Check if file exists
	fileInfo, err := os.Stat(nzbPath)
	if err != nil {
		return "", fmt.Errorf("failed to stat NZB file: %w", err)
	}
	if fileInfo.IsDir() {
		return "", fmt.Errorf("NZB path is a directory, not a file")
	}

	// Open the NZB file
	file, err := os.Open(nzbPath)
	if err != nil {
		return "", fmt.Errorf("failed to open NZB file: %w", err)
	}
	defer file.Close()

	// Create multipart form data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add the file
	filename := filepath.Base(nzbPath)
	part, err := writer.CreateFormFile("nzbfile", filename)
	if err != nil {
		return "", fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return "", fmt.Errorf("failed to copy file data: %w", err)
	}

	// Add category if provided
	if category != nil && *category != "" {
		if err := writer.WriteField("cat", *category); err != nil {
			return "", fmt.Errorf("failed to write category field: %w", err)
		}
	}

	// Add priority if provided
	if priority != nil && *priority != "" {
		if err := writer.WriteField("priority", *priority); err != nil {
			return "", fmt.Errorf("failed to write priority field: %w", err)
		}
	}

	// Close the multipart writer
	if err := writer.Close(); err != nil {
		return "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Build the request URL
	requestURL, err := c.buildAddFileURL(host, apiKey)
	if err != nil {
		return "", fmt.Errorf("failed to build request URL: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequest("POST", requestURL, body)
	if err != nil {
		return "", fmt.Errorf("failed to create HTTP request: %w", err)
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Send the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request to SABnzbd: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("SABnzbd returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the JSON response
	var apiResp SABnzbdAPIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("failed to parse SABnzbd response: %w", err)
	}

	// Check if the request was successful
	if !apiResp.Status {
		errorMsg := "unknown error"
		if apiResp.Error != nil {
			errorMsg = *apiResp.Error
		}
		return "", fmt.Errorf("SABnzbd API error: %s", errorMsg)
	}

	// Extract the NZO ID
	if len(apiResp.NzoIds) == 0 {
		return "", fmt.Errorf("SABnzbd did not return an NZO ID")
	}

	return apiResp.NzoIds[0], nil
}

// buildAddFileURL constructs the SABnzbd API URL for adding files
func (c *SABnzbdClient) buildAddFileURL(host, apiKey string) (string, error) {
	// Parse the host URL to ensure it's valid
	baseURL, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid SABnzbd host URL: %w", err)
	}

	// Build the query parameters
	params := url.Values{}
	params.Add("mode", "addfile")
	params.Add("apikey", apiKey)
	params.Add("output", "json")

	// Construct the full URL
	fullURL := fmt.Sprintf("%s/api?%s", baseURL.String(), params.Encode())
	return fullURL, nil
}
