package rclonecli

import (
	"context"
	"net/http"
	"testing"

	"github.com/steinfletcher/apitest"
	"github.com/stretchr/testify/assert"
)

func TestRefreshCache(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	defer apitest.NewMock().
		HttpClient(hc).
		Postf("%s/vfs/refresh", host).
		Header("Content-Type", "application/json").
		Body(`{"_async":"true","recursive":"false"}`).
		RespondWith().
		Status(http.StatusOK).
		Body(`{"status":"ok"}`).
		EndStandalone()()

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "", true, false)
	assert.NoError(t, err)
}

func TestRefreshCacheWithDir(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	defer apitest.NewMock().
		HttpClient(hc).
		Postf("%s/vfs/refresh", host).
		Header("Content-Type", "application/json").
		Body(`{"_async":"true","dir":"/foo","recursive":"false"}`).
		RespondWith().
		Status(http.StatusOK).
		Body(`{"status":"ok"}`).
		EndStandalone()()

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", true, false)
	assert.NoError(t, err)
}

func TestRefreshCacheWithDirAndRecursive(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	defer apitest.NewMock().
		HttpClient(hc).
		Postf("%s/vfs/refresh", host).
		Header("Content-Type", "application/json").
		Body(`{"_async":"true","dir":"/foo","recursive":"true"}`).
		RespondWith().
		Status(http.StatusOK).
		Body(`{"status":"ok"}`).
		EndStandalone()()

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", true, true)
	assert.NoError(t, err)
}

func TestRefreshCacheWithDirAndRecursiveAndNotAsync(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	defer apitest.NewMock().
		HttpClient(hc).
		Postf("%s/vfs/refresh", host).
		Header("Content-Type", "application/json").
		Body(`{"_async":"false","dir":"/foo","recursive":"true"}`).
		RespondWith().
		Status(http.StatusOK).
		Body(`{"status":"ok"}`).
		EndStandalone()()

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", false, true)
	assert.NoError(t, err)
}

func TestRefreshCacheWithError(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	defer apitest.NewMock().
		HttpClient(hc).
		Postf("%s/vfs/refresh", host).
		Header("Content-Type", "application/json").
		Body(`{"_async":"false","dir":"/foo","recursive":"true"}`).
		RespondWith().
		Status(http.StatusInternalServerError).
		Body(`{"error":"error"}`).
		EndStandalone()()

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", false, true)
	assert.Error(t, err)
}

func TestRefreshCacheDisabled(t *testing.T) {
	host := "http://localhost:5572"
	hc := &http.Client{}

	// No mock setup since no HTTP call should be made when VFS is disabled
	client := NewRcloneRcClient(&Config{
		VFSEnabled: false,
		VFSUrl:     host,
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", true, false)
	assert.NoError(t, err) // Should return nil without making HTTP call
}

func TestRefreshCacheNoURL(t *testing.T) {
	hc := &http.Client{}

	client := NewRcloneRcClient(&Config{
		VFSEnabled: true,
		VFSUrl:     "", // Empty URL
	}, hc)

	err := client.RefreshCache(context.Background(), "/foo", true, false)
	assert.Error(t, err) // Should error because URL is not configured
	assert.Contains(t, err.Error(), "VFS URL is not configured")
}

func TestBuildVFSUrl(t *testing.T) {
	tests := []struct {
		name        string
		vfsUrl      string
		vfsUser     string
		vfsPass     string
		expected    string
		expectError bool
	}{
		// Basic cases without auth
		{
			name:     "URL with http protocol",
			vfsUrl:   "http://example.com",
			expected: "http://example.com",
		},
		{
			name:     "URL with https protocol",
			vfsUrl:   "https://example.com",
			expected: "https://example.com",
		},
		{
			name:     "URL without protocol defaults to http",
			vfsUrl:   "example.com",
			expected: "http://example.com",
		},
		{
			name:     "URL with port",
			vfsUrl:   "example.com:8080",
			expected: "http://example.com:8080",
		},
		{
			name:     "URL with https and port",
			vfsUrl:   "https://example.com:443",
			expected: "https://example.com:443",
		},
		{
			name:     "URL with path",
			vfsUrl:   "http://example.com/api",
			expected: "http://example.com/api",
		},

		// Cases with authentication
		{
			name:     "HTTP URL with auth credentials",
			vfsUrl:   "http://example.com",
			vfsUser:  "user",
			vfsPass:  "pass",
			expected: "http://user:pass@example.com",
		},
		{
			name:     "HTTPS URL with auth credentials",
			vfsUrl:   "https://example.com",
			vfsUser:  "user",
			vfsPass:  "pass",
			expected: "https://user:pass@example.com",
		},
		{
			name:     "URL without protocol with auth",
			vfsUrl:   "example.com",
			vfsUser:  "user",
			vfsPass:  "pass",
			expected: "http://user:pass@example.com",
		},
		{
			name:     "URL with port and auth",
			vfsUrl:   "https://example.com:9000",
			vfsUser:  "admin",
			vfsPass:  "secret",
			expected: "https://admin:secret@example.com:9000",
		},
		{
			name:     "URL with path and auth",
			vfsUrl:   "http://example.com/rclone",
			vfsUser:  "user",
			vfsPass:  "pass",
			expected: "http://user:pass@example.com/rclone",
		},

		// Special characters in credentials
		{
			name:     "Auth with special characters",
			vfsUrl:   "http://example.com",
			vfsUser:  "user@domain",
			vfsPass:  "p@ss:w0rd!",
			expected: "http://user%40domain:p%40ss%3Aw0rd%21@example.com",
		},

		// Override existing auth
		{
			name:     "Override existing auth in URL",
			vfsUrl:   "http://olduser:oldpass@example.com",
			vfsUser:  "newuser",
			vfsPass:  "newpass",
			expected: "http://newuser:newpass@example.com",
		},
		{
			name:     "Preserve existing auth when no config auth provided",
			vfsUrl:   "http://existinguser:existingpass@example.com",
			expected: "http://existinguser:existingpass@example.com",
		},

		// IPv6 addresses (only with explicit protocol)
		{
			name:     "IPv6 address with https",
			vfsUrl:   "https://[2001:db8::1]:443",
			expected: "https://[2001:db8::1]:443",
		},
		{
			name:     "IPv6 address with auth",
			vfsUrl:   "http://[::1]:8080",
			vfsUser:  "user",
			vfsPass:  "pass",
			expected: "http://user:pass@[::1]:8080",
		},
		{
			name:        "IPv6 address without protocol should fail",
			vfsUrl:      "[::1]:8080",
			expectError: true,
		},

		// Error cases
		{
			name:        "Empty VFS URL",
			vfsUrl:      "",
			expectError: true,
		},
		{
			name:        "Invalid URL scheme",
			vfsUrl:      "ftp://example.com",
			expectError: true,
		},
		{
			name:        "URL with no host",
			vfsUrl:      "http://",
			expectError: true,
		},
		{
			name:        "Malformed URL with invalid port",
			vfsUrl:      "http://example.com:abc",
			expectError: true,
		},
		{
			name:     "Only partial auth - user without pass",
			vfsUrl:   "http://example.com",
			vfsUser:  "user",
			vfsPass:  "",
			expected: "http://example.com", // No auth added when pass is empty
		},
		{
			name:     "Only partial auth - pass without user",
			vfsUrl:   "http://example.com",
			vfsUser:  "",
			vfsPass:  "pass",
			expected: "http://example.com", // No auth added when user is empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := &Config{
				VFSUrl:  tt.vfsUrl,
				VFSUser: tt.vfsUser,
				VFSPass: tt.vfsPass,
			}

			client := &rcloneRcClient{
				config:     config,
				httpClient: &http.Client{},
			}

			result, err := client.buildVFSUrl()

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none. Result: %s", result)
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %q, got %q", tt.expected, result)
			}
		})
	}
}
