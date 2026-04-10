package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
				MountPath: "/mnt/remotes/altmount",
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
				MountPath: "/mnt/remotes/altmount",
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

func TestConfig_GetWebhookBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "explicitly set",
			config: Config{
				Arrs: ArrsConfig{
					WebhookBaseURL: "http://custom:1234",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
			},
			expected: "http://custom:1234",
		},
		{
			name: "default with port 8080",
			config: Config{
				Arrs: ArrsConfig{
					WebhookBaseURL: "",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
			},
			expected: "http://altmount:8080",
		},
		{
			name: "default with port 8084",
			config: Config{
				Arrs: ArrsConfig{
					WebhookBaseURL: "",
				},
				WebDAV: WebDAVConfig{
					Port: 8084,
				},
			},
			expected: "http://altmount:8084",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.config.GetWebhookBaseURL())
		})
	}
}

func TestConfig_GetDownloadClientBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		config   Config
		expected string
	}{
		{
			name: "explicitly set",
			config: Config{
				SABnzbd: SABnzbdConfig{
					DownloadClientBaseURL: "http://custom:1234/sab",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
			},
			expected: "http://custom:1234/sab",
		},
		{
			name: "default with port 8080",
			config: Config{
				SABnzbd: SABnzbdConfig{
					DownloadClientBaseURL: "",
				},
				WebDAV: WebDAVConfig{
					Port: 8080,
				},
			},
			expected: "http://altmount:8080/sabnzbd",
		},
		{
			name: "default with port 8084",
			config: Config{
				SABnzbd: SABnzbdConfig{
					DownloadClientBaseURL: "",
				},
				WebDAV: WebDAVConfig{
					Port: 8084,
				},
			},
			expected: "http://altmount:8084/sabnzbd",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.config.GetDownloadClientBaseURL())
		})
	}
}

func TestFuseConfig_UseReadAt_DefaultsAndHelper(t *testing.T) {
	cfg := DefaultConfig("/tmp/altmount-test-fuse-use-read-at")
	require.NoError(t, cfg.Validate())
	require.NotNil(t, cfg.Fuse.UseReadAt)
	assert.True(t, *cfg.Fuse.UseReadAt)
	assert.True(t, cfg.Fuse.UseReadAtEnabled())

	cfgOff := DefaultConfig("/tmp/altmount-test-fuse-use-read-at-off")
	disabled := false
	cfgOff.Fuse.UseReadAt = &disabled
	require.NoError(t, cfgOff.Validate())
	assert.False(t, cfgOff.Fuse.UseReadAtEnabled())

	var unset FuseConfig
	assert.True(t, unset.UseReadAtEnabled())
	unset.UseReadAt = &disabled
	assert.False(t, unset.UseReadAtEnabled())
}

