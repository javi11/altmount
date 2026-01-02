package nzblnk

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// NZBLinkParams represents parsed nzblnk:// parameters
type NZBLinkParams struct {
	Title    string   // t param (required) - content title used for search
	Header   string   // h param (required) - header pattern for verification
	Groups   []string // g param (optional, can repeat) - newsgroups
	Password string   // p param (optional) - archive password
}

// SearchResult represents a search result from an indexer
type SearchResult struct {
	ID       string  // Unique identifier for downloading
	Title    string  // Title of the NZB
	Size     int64   // Size in bytes (0 if unknown)
	Complete float64 // Percentage complete (0-100, 0 if unknown)
}

// Indexer interface for NZB search engines
type Indexer interface {
	Name() string
	Search(ctx context.Context, title string) (*SearchResult, error)
	DownloadNZB(ctx context.Context, id string) ([]byte, error)
}

// Resolver searches indexers to find NZB files for NZBLNK links
type Resolver struct {
	httpClient *http.Client
	indexers   []Indexer
}

// NewResolver creates a new NZBLNK resolver with default indexers
func NewResolver() *Resolver {
	client := &http.Client{
		Timeout: 30 * time.Second,
	}

	return &Resolver{
		httpClient: client,
		indexers: []Indexer{
			NewNZBKingIndexer(client),
			NewNZBIndexIndexer(client),
		},
	}
}

// ParseNZBLink parses an nzblnk:// URL and extracts parameters
func ParseNZBLink(link string) (*NZBLinkParams, error) {
	// Normalize the link format
	link = strings.TrimSpace(link)

	// Check for valid scheme
	if !strings.HasPrefix(link, "nzblnk:?") {
		return nil, fmt.Errorf("invalid NZBLNK format: must start with 'nzblnk:?'")
	}

	// Parse the query string part
	queryPart := strings.TrimPrefix(link, "nzblnk:?")
	values, err := url.ParseQuery(queryPart)
	if err != nil {
		return nil, fmt.Errorf("failed to parse NZBLNK parameters: %w", err)
	}

	// Extract required parameters
	title := values.Get("t")
	if title == "" {
		return nil, fmt.Errorf("missing required parameter 't' (title)")
	}

	header := values.Get("h")
	if header == "" {
		return nil, fmt.Errorf("missing required parameter 'h' (header)")
	}

	// Extract optional parameters
	params := &NZBLinkParams{
		Title:    title,
		Header:   header,
		Groups:   values["g"], // Can have multiple 'g' params
		Password: values.Get("p"),
	}

	return params, nil
}

// ResolveResult represents the result of resolving an NZBLNK
type ResolveResult struct {
	NZBContent []byte // The downloaded NZB file content
	Title      string // The title from the link
	Password   string // Optional password from the link
	Indexer    string // Which indexer was used
}

// Resolve attempts to resolve an NZBLNK and download the NZB file
func (r *Resolver) Resolve(ctx context.Context, link string) (*ResolveResult, error) {
	// Parse the link
	params, err := ParseNZBLink(link)
	if err != nil {
		return nil, err
	}

	slog.DebugContext(ctx, "Resolving NZBLNK",
		"title", params.Title,
		"header", params.Header,
		"groups", params.Groups)

	// Try each indexer until one succeeds
	var lastErr error
	for _, indexer := range r.indexers {
		slog.DebugContext(ctx, "Trying indexer",
			"indexer", indexer.Name(),
			"header", params.Header)

		// Search for the NZB using the header parameter
		result, err := indexer.Search(ctx, params.Header)
		if err != nil {
			slog.DebugContext(ctx, "Indexer search failed",
				"indexer", indexer.Name(),
				"error", err)
			lastErr = fmt.Errorf("%s: %w", indexer.Name(), err)
			continue
		}

		if result == nil {
			slog.DebugContext(ctx, "Indexer returned no results",
				"indexer", indexer.Name())
			lastErr = fmt.Errorf("%s: no results found", indexer.Name())
			continue
		}

		slog.DebugContext(ctx, "Indexer found result",
			"indexer", indexer.Name(),
			"result_id", result.ID,
			"result_title", result.Title)

		// Download the NZB
		nzbContent, err := indexer.DownloadNZB(ctx, result.ID)
		if err != nil {
			slog.DebugContext(ctx, "Failed to download NZB",
				"indexer", indexer.Name(),
				"error", err)
			lastErr = fmt.Errorf("%s: failed to download NZB: %w", indexer.Name(), err)
			continue
		}

		// Validate NZB content
		if !isValidNZB(nzbContent) {
			slog.DebugContext(ctx, "Downloaded content is not valid NZB",
				"indexer", indexer.Name(),
				"content_size", len(nzbContent))
			lastErr = fmt.Errorf("%s: downloaded content is not valid NZB", indexer.Name())
			continue
		}

		slog.InfoContext(ctx, "NZBLNK resolved successfully",
			"indexer", indexer.Name(),
			"title", params.Title,
			"nzb_size", len(nzbContent))

		return &ResolveResult{
			NZBContent: nzbContent,
			Title:      params.Title,
			Password:   params.Password,
			Indexer:    indexer.Name(),
		}, nil
	}

	if lastErr != nil {
		return nil, fmt.Errorf("all indexers failed: %w", lastErr)
	}
	return nil, fmt.Errorf("no indexers available")
}

// isValidNZB performs basic validation on NZB content
func isValidNZB(content []byte) bool {
	if len(content) == 0 {
		return false
	}

	// Check for XML declaration or nzb root element
	s := strings.ToLower(string(content[:min(len(content), 500)]))
	return strings.Contains(s, "<?xml") || strings.Contains(s, "<nzb")
}
