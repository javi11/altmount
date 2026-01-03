package nzblnk

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	// User-Agent to use for indexer requests (browser-like)
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

	// Maximum response size to prevent memory issues (10MB)
	maxResponseSize = 10 * 1024 * 1024
)

// NZBKingIndexer searches nzbking.com for NZB files
type NZBKingIndexer struct {
	client *http.Client
}

// NewNZBKingIndexer creates a new NZBKing indexer
func NewNZBKingIndexer(client *http.Client) *NZBKingIndexer {
	return &NZBKingIndexer{client: client}
}

// Name returns the indexer name
func (n *NZBKingIndexer) Name() string {
	return "nzbking"
}

// Search searches nzbking.com for the given query
func (n *NZBKingIndexer) Search(ctx context.Context, query string) (*SearchResult, error) {
	// Build search URL
	searchURL := fmt.Sprintf("https://www.nzbking.com/?q=%s", url.QueryEscape(query))

	slog.DebugContext(ctx, "NZBKing search", "url", searchURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	slog.DebugContext(ctx, "NZBKing response", "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	// Read response with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse HTML to extract first result ID
	// Look for pattern: href="/details:[hex_id]/"
	re := regexp.MustCompile(`href="/details:([a-f0-9]+)/"`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, fmt.Errorf("no results found")
	}

	id := string(matches[1])

	slog.DebugContext(ctx, "NZBKing found result", "id", id)

	return &SearchResult{
		ID:    id,
		Title: query,
	}, nil
}

// DownloadNZB downloads the NZB file from nzbking.com
func (n *NZBKingIndexer) DownloadNZB(ctx context.Context, id string) ([]byte, error) {
	// Build download URL
	downloadURL := fmt.Sprintf("https://www.nzbking.com/nzb:%s/", id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Read response with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read NZB: %w", err)
	}

	return body, nil
}

// NZBIndexIndexer searches nzbindex.com for NZB files
type NZBIndexIndexer struct {
	client *http.Client
}

// NewNZBIndexIndexer creates a new NZBIndex indexer
func NewNZBIndexIndexer(client *http.Client) *NZBIndexIndexer {
	return &NZBIndexIndexer{client: client}
}

// Name returns the indexer name
func (n *NZBIndexIndexer) Name() string {
	return "nzbindex"
}

// Search searches nzbindex.com for the given query
func (n *NZBIndexIndexer) Search(ctx context.Context, query string) (*SearchResult, error) {
	// Build search URL (HTML endpoint)
	searchURL := fmt.Sprintf("https://nzbindex.com/search?q=%s&minage=&maxage=&minsize=&maxsize=&sort=agedesc&max=25&poster=&groups=", url.QueryEscape(query))

	slog.DebugContext(ctx, "NZBIndex search", "url", searchURL)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	slog.DebugContext(ctx, "NZBIndex response", "status", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	// Read response with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Parse HTML to extract first result ID
	// Look for download link pattern: href="/download/[ID]"
	re := regexp.MustCompile(`href="/download/(\d+)"`)
	matches := re.FindSubmatch(body)
	if len(matches) < 2 {
		return nil, fmt.Errorf("no results found")
	}

	id := string(matches[1])

	slog.DebugContext(ctx, "NZBIndex found result", "id", id)

	return &SearchResult{
		ID:    id,
		Title: query,
	}, nil
}


// DownloadNZB downloads the NZB file from nzbindex.com
func (n *NZBIndexIndexer) DownloadNZB(ctx context.Context, id string) ([]byte, error) {
	// Build download URL
	downloadURL := fmt.Sprintf("https://nzbindex.com/download/%s", id)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", userAgent)

	resp, err := n.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download returned status %d", resp.StatusCode)
	}

	// Check content type - should be NZB
	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "nzb") && !strings.Contains(contentType, "xml") && !strings.Contains(contentType, "octet-stream") {
		return nil, fmt.Errorf("unexpected content type: %s", contentType)
	}

	// Read response with size limit
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return nil, fmt.Errorf("failed to read NZB: %w", err)
	}

	return body, nil
}
