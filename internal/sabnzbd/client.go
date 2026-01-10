package sabnzbd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"github.com/javi11/altmount/internal/httpclient"
)

// SABnzbdClient handles communication with external SABnzbd instances
type SABnzbdClient struct {
	httpClient *http.Client
}

// NewSABnzbdClient creates a new SABnzbd client with reasonable timeouts
func NewSABnzbdClient() *SABnzbdClient {
	return &SABnzbdClient{
		httpClient: httpclient.NewLong(), // 60 second timeout for file uploads
	}
}

// SABnzbdHistoryResponse represents a response from SABnzbd history API
type SABnzbdHistoryResponse struct {
	Status  bool                        `json:"status"`
	Error   *string                     `json:"error,omitempty"`
	History SABnzbdHistorySlotsWrapper `json:"history,omitempty"`
}

// SABnzbdHistorySlotsWrapper wraps the history slots
type SABnzbdHistorySlotsWrapper struct {
	Slots []SABnzbdHistorySlot `json:"slots"`
}

// SABnzbdHistorySlot represents a history item
type SABnzbdHistorySlot struct {
	NzoID       string  `json:"nzo_id"`
	Name        string  `json:"name"`
	Status      string  `json:"status"`
	ActionLine  string  `json:"action_line"`
	FailMessage *string `json:"fail_message,omitempty"`
}

// SABnzbdQueueResponse represents a response from SABnzbd queue API
type SABnzbdQueueResponse struct {
	Status bool                     `json:"status"`
	Error  *string                  `json:"error,omitempty"`
	Queue  SABnzbdQueueSlotsWrapper `json:"queue,omitempty"`
}

// SABnzbdQueueSlotsWrapper wraps the queue slots
type SABnzbdQueueSlotsWrapper struct {
	Slots []SABnzbdQueueSlot `json:"slots"`
}

// SABnzbdQueueSlot represents a queue item in SABnzbd
type SABnzbdQueueSlot struct {
	NzoID      string `json:"nzo_id"`
	Name       string `json:"filename"`
	Status     string `json:"status"`
	Percentage string `json:"percentage"`
}

// SABnzbdAPIResponse represents a response from SABnzbd API (used by addfile)
type SABnzbdAPIResponse struct {
	Status bool     `json:"status"`
	Error  *string  `json:"error,omitempty"`
	NzoIds []string `json:"nzo_ids,omitempty"`
}

// Priority constants for SABnzbd downloads
const (
	PriorityDefault = "-100" // Default priority
	PriorityPaused  = "-2"   // Paused download
	PriorityLow     = "-1"   // Low priority
	PriorityNormal  = "0"    // Normal priority
	PriorityHigh    = "1"    // High priority
	PriorityForce   = "2"    // Force priority
)

// SendNZBFile sends an NZB file to an external SABnzbd instance
// Returns the NZO ID assigned by SABnzbd, or an error
// Priority values: "-100" (default), "-2" (paused), "-1" (low), "0" (normal), "1" (high), "2" (force)
func (c *SABnzbdClient) SendNZBFile(ctx context.Context, host, apiKey, nzbPath string, category *string, priority *string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

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
	req, err := http.NewRequestWithContext(ctx, "POST", requestURL, body)
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

// GetHistory retrieves the download history from SABnzbd
// Returns history slots or an error
func (c *SABnzbdClient) GetHistory(ctx context.Context, host, apiKey string) ([]SABnzbdHistorySlot, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Validate inputs
	if host == "" {
		return nil, fmt.Errorf("SABnzbd host cannot be empty")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("SABnzbd API key cannot be empty")
	}

	// Build the request URL
	requestURL, err := c.buildHistoryURL(host, apiKey)
	if err != nil {
		return nil, fmt.Errorf("failed to build request URL: %w", err)
	}

	// Create the HTTP request
	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	// Send the request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request to SABnzbd: %w", err)
	}
	defer resp.Body.Close()

	// Read the response body
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SABnzbd returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// Parse the JSON response
	var apiResp SABnzbdHistoryResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to parse SABnzbd response: %w", err)
	}

	// Check if the request was successful
	if !apiResp.Status {
		errorMsg := "unknown error"
		if apiResp.Error != nil {
			errorMsg = *apiResp.Error
		}
		return nil, fmt.Errorf("SABnzbd API error: %s", errorMsg)
	}

	return apiResp.History.Slots, nil
}

// buildHistoryURL constructs the SABnzbd API URL for getting history
func (c *SABnzbdClient) buildHistoryURL(host, apiKey string) (string, error) {
	// Parse the host URL to ensure it's valid
	baseURL, err := url.Parse(host)
	if err != nil {
		return "", fmt.Errorf("invalid SABnzbd host URL: %w", err)
	}

	// Build the query parameters
	params := url.Values{}
	params.Add("mode", "history")
	params.Add("apikey", apiKey)
	params.Add("output", "json")
	params.Add("limit", "100") // Get recent history

	// Construct the full URL
	fullURL := fmt.Sprintf("%s/api?%s", baseURL.String(), params.Encode())
	return fullURL, nil
}

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

// GetQueue retrieves the download queue from SABnzbd
func (c *SABnzbdClient) GetQueue(ctx context.Context, host, apiKey string) ([]SABnzbdQueueSlot, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if host == "" || apiKey == "" {
		return nil, fmt.Errorf("SABnzbd host and API key required")
	}

	baseURL, err := url.Parse(host)
	if err != nil {
		return nil, fmt.Errorf("invalid SABnzbd host URL: %w", err)
	}

	params := url.Values{}
	params.Add("mode", "queue")
	params.Add("apikey", apiKey)
	params.Add("output", "json")
	requestURL := fmt.Sprintf("%s/api?%s", baseURL.String(), params.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", requestURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("SABnzbd returned HTTP %d", resp.StatusCode)
	}

	var apiResp SABnzbdQueueResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, err
	}

	return apiResp.Queue.Slots, nil
}
