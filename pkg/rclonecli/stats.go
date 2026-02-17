package rclonecli

import (
	"context"
	"encoding/json"
	"fmt"
)

type TransferringStat struct {
	Bytes    int64   `json:"bytes"`
	ETA      int64   `json:"eta"`
	Name     string  `json:"name"`
	Speed    float64 `json:"speed"`
	Size     int64   `json:"size"`
	Progress float64 `json:"progress"`
}

type VersionResponse struct {
	Arch    string `json:"arch"`
	Version string `json:"version"`
	OS      string `json:"os"`
}

type CoreStatsResponse struct {
	Bytes          int64              `json:"bytes"`
	Checks         int                `json:"checks"`
	DeletedDirs    int                `json:"deletedDirs"`
	Deletes        int                `json:"deletes"`
	ElapsedTime    float64            `json:"elapsedTime"`
	Errors         int                `json:"errors"`
	Eta            int                `json:"eta"`
	Speed          float64            `json:"speed"`
	TotalBytes     int64              `json:"totalBytes"`
	TotalChecks    int                `json:"totalChecks"`
	TotalTransfers int                `json:"totalTransfers"`
	TransferTime   float64            `json:"transferTime"`
	Transfers      int                `json:"transfers"`
	Transferring   []TransferringStat `json:"transferring,omitempty"`
}

type MemoryStats struct {
	Sys        int   `json:"Sys"`
	TotalAlloc int64 `json:"TotalAlloc"`
}

type BandwidthStats struct {
	BytesPerSecond int64  `json:"bytesPerSecond"`
	Rate           string `json:"rate"`
}

// Stats represents rclone statistics
type Stats struct {
	Enabled   bool                  `json:"enabled"`
	Ready     bool                  `json:"server_ready"`
	Core      CoreStatsResponse     `json:"core"`
	Memory    MemoryStats           `json:"memory"`
	Mount     map[string]*MountInfo `json:"mount"`
	Bandwidth BandwidthStats        `json:"bandwidth"`
	Version   VersionResponse       `json:"version"`
}

// GetStats retrieves statistics from the rclone RC server
func (m *Manager) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{}
	stats.Ready = m.IsReady()
	stats.Enabled = true

	coreStats, err := m.GetCoreStats(ctx)
	if err == nil {
		stats.Core = *coreStats
	}

	// Get memory usage
	memStats, err := m.GetMemoryUsage(ctx)
	if err == nil {
		stats.Memory = *memStats
	}
	// Get bandwidth stats
	bwStats, err := m.GetBandwidthStats(ctx)
	if err == nil && bwStats != nil {
		stats.Bandwidth = *bwStats
	} else {
		fmt.Println("Failed to get rclone stats", err)
	}

	// Get version info
	versionResp, err := m.GetVersion(ctx)
	if err == nil {
		stats.Version = *versionResp
	}

	// Get mount info
	stats.Mount = m.GetAllMounts()
	return stats, nil
}

func (m *Manager) GetCoreStats(ctx context.Context) (*CoreStatsResponse, error) {
	if !m.IsReady() {
		return nil, fmt.Errorf("rclone RC server not ready")
	}

	req := RCRequest{
		Command: "core/stats",
		Args:    map[string]any{},
	}

	resp, err := m.makeRequest(req, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get core stats: %w", err)
	}
	defer resp.Body.Close()

	var coreStats CoreStatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&coreStats); err != nil {
		return nil, fmt.Errorf("failed to decode core stats response: %w", err)
	}
	return &coreStats, nil
}

// GetMemoryUsage returns memory usage statistics
func (m *Manager) GetMemoryUsage(ctx context.Context) (*MemoryStats, error) {
	if !m.IsReady() {
		return nil, fmt.Errorf("rclone RC server not ready")
	}

	req := RCRequest{
		Command: "core/memstats",
		Args:    map[string]any{},
	}

	resp, err := m.makeRequest(req, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory stats: %w", err)
	}
	defer resp.Body.Close()
	var memStats MemoryStats

	if err := json.NewDecoder(resp.Body).Decode(&memStats); err != nil {
		return nil, fmt.Errorf("failed to decode memory stats response: %w", err)
	}
	return &memStats, nil
}

// GetBandwidthStats returns bandwidth usage for all transfers
func (m *Manager) GetBandwidthStats(ctx context.Context) (*BandwidthStats, error) {
	if !m.IsReady() {
		return nil, fmt.Errorf("rclone RC server not ready")
	}

	req := RCRequest{
		Command: "core/bwlimit",
		Args:    map[string]any{},
	}

	resp, err := m.makeRequest(req, false)
	if err != nil {
		// Bandwidth stats might not be available, return empty
		return nil, nil
	}
	defer resp.Body.Close()
	var bwStats BandwidthStats
	if err := json.NewDecoder(resp.Body).Decode(&bwStats); err != nil {
		return nil, fmt.Errorf("failed to decode bandwidth stats response: %w", err)
	}
	return &bwStats, nil
}

// GetVersion returns rclone version information
func (m *Manager) GetVersion(ctx context.Context) (*VersionResponse, error) {
	if !m.IsReady() {
		return nil, fmt.Errorf("rclone RC server not ready")
	}

	req := RCRequest{
		Command: "core/version",
		Args:    map[string]any{},
	}

	resp, err := m.makeRequest(req, false)
	if err != nil {
		return nil, fmt.Errorf("failed to get version: %w", err)
	}
	defer resp.Body.Close()
	var versionResp VersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return nil, fmt.Errorf("failed to decode version response: %w", err)
	}
	return &versionResp, nil
}
