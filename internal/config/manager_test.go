package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate_MountPaths(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "both enabled same path - error",
			config: &Config{
				MountPath: "/mnt/altmount",
				RClone: RCloneConfig{
					MountEnabled: &enabled,
				},
				Fuse: FuseConfig{
					Enabled:   &enabled,
					MountPath: "/mnt/altmount",
				},
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxDownloadWorkers: 15,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					ImportCacheSizeMB:              64,
					SegmentSamplePercentage:        1,
					ImportStrategy:                 ImportStrategyNone,
				},
				Health: HealthConfig{
					CheckIntervalSeconds:          5,
					MaxConnectionsForHealthChecks: 5,
					MaxConcurrentJobs:             1,
					SegmentSamplePercentage:       5,
				},
			},
			wantErr:     true,
			errContains: "rclone mount and native mount cannot use the same path",
		},
		{
			name: "both enabled same path via fallback - error",
			config: &Config{
				MountPath: "/mnt/altmount",
				RClone: RCloneConfig{
					MountEnabled: &enabled,
				},
				Fuse: FuseConfig{
					Enabled:   &enabled,
					MountPath: "", // fallback to MountPath
				},
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxDownloadWorkers: 15,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					ImportCacheSizeMB:              64,
					SegmentSamplePercentage:        1,
					ImportStrategy:                 ImportStrategyNone,
				},
				Health: HealthConfig{
					CheckIntervalSeconds:          5,
					MaxConnectionsForHealthChecks: 5,
					MaxConcurrentJobs:             1,
					SegmentSamplePercentage:       5,
				},
			},
			wantErr:     true,
			errContains: "rclone mount and native mount cannot use the same path",
		},
		{
			name: "both enabled different paths - ok",
			config: &Config{
				MountPath: "/mnt/rclone",
				RClone: RCloneConfig{
					MountEnabled: &enabled,
				},
				Fuse: FuseConfig{
					Enabled:   &enabled,
					MountPath: "/mnt/fuse",
				},
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxDownloadWorkers: 15,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					ImportCacheSizeMB:              64,
					SegmentSamplePercentage:        1,
					ImportStrategy:                 ImportStrategyNone,
				},
				Health: HealthConfig{
					CheckIntervalSeconds:          5,
					MaxConnectionsForHealthChecks: 5,
					MaxConcurrentJobs:             1,
					SegmentSamplePercentage:       5,
				},
			},
			wantErr: false,
		},
		{
			name: "only fuse enabled same path - ok",
			config: &Config{
				MountPath: "/mnt/altmount",
				RClone: RCloneConfig{
					MountEnabled: &disabled,
				},
				Fuse: FuseConfig{
					Enabled:   &enabled,
					MountPath: "", // fallback to MountPath
				},
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxDownloadWorkers: 15,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					ImportCacheSizeMB:              64,
					SegmentSamplePercentage:        1,
					ImportStrategy:                 ImportStrategyNone,
				},
				Health: HealthConfig{
					CheckIntervalSeconds:          5,
					MaxConnectionsForHealthChecks: 5,
					MaxConcurrentJobs:             1,
					SegmentSamplePercentage:       5,
				},
			},
			wantErr: false,
		},
		{
			name: "only rclone enabled same path - ok",
			config: &Config{
				MountPath: "/mnt/altmount",
				RClone: RCloneConfig{
					MountEnabled: &enabled,
				},
				Fuse: FuseConfig{
					Enabled:   &disabled,
					MountPath: "/mnt/altmount",
				},
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxDownloadWorkers: 15,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					ImportCacheSizeMB:              64,
					SegmentSamplePercentage:        1,
					ImportStrategy:                 ImportStrategyNone,
				},
				Health: HealthConfig{
					CheckIntervalSeconds:          5,
					MaxConnectionsForHealthChecks: 5,
					MaxConcurrentJobs:             1,
					SegmentSamplePercentage:       5,
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
