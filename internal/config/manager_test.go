package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConfig_Validate_MountPaths(t *testing.T) {
	tests := []struct {
		name        string
		config      *Config
		wantErr     bool
		errContains string
	}{
		{
			name: "mount type fuse - ok",
			config: &Config{
				MountType: MountTypeFuse,
				MountPath: "/mnt/altmount",
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxPrefetch: 30,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					MaxDownloadPrefetch:            3,
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
			name: "mount type rclone - ok",
			config: &Config{
				MountType: MountTypeRClone,
				MountPath: "/mnt/altmount",
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxPrefetch: 30,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					MaxDownloadPrefetch:            3,
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
			name: "mount type none - ok",
			config: &Config{
				MountType: MountTypeNone,
				Metadata: MetadataConfig{
					RootPath: "/metadata",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
				Streaming: StreamingConfig{
					MaxPrefetch: 30,
				},
				Import: ImportConfig{
					MaxProcessorWorkers:            2,
					QueueProcessingIntervalSeconds: 5,
					MaxImportConnections:           5,
					MaxDownloadPrefetch:            3,
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
