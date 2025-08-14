package nzbfilesystem

import (
	"strings"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/encryption/headers"
	"github.com/javi11/altmount/internal/encryption/rclone"
	"github.com/javi11/nntppool"
)

// NzbRemoteFileConfig holds configuration for NzbRemoteFile
type NzbRemoteFileConfig struct {
	GlobalPassword string // Global password for .bin files
	GlobalSalt     string // Global salt for .bin files
}

// NewNzbRemoteFile creates a new NZB remote file handler with default config
func NewNzbRemoteFile(db *database.DB, cp nntppool.UsenetConnectionPool, maxDownloadWorkers int) *NzbRemoteFile {
	return NewNzbRemoteFileWithConfig(db, cp, maxDownloadWorkers, NzbRemoteFileConfig{})
}

// NewNzbRemoteFileWithConfig creates a new NZB remote file handler with configuration
func NewNzbRemoteFileWithConfig(db *database.DB, cp nntppool.UsenetConnectionPool, maxDownloadWorkers int, config NzbRemoteFileConfig) *NzbRemoteFile {
	// Initialize rclone cipher with global credentials for encrypted files
	rcloneConfig := &encryption.Config{
		RclonePassword: config.GlobalPassword, // Global password fallback
		RcloneSalt:     config.GlobalSalt,     // Global salt fallback
	}

	rcloneCipher, _ := rclone.NewRcloneCipher(rcloneConfig)
	headersCipher, _ := headers.NewHeadersCipher()

	return &NzbRemoteFile{
		db:                 db,
		cp:                 cp,
		maxDownloadWorkers: maxDownloadWorkers,
		rcloneCipher:       rcloneCipher,
		headersCipher:      headersCipher,
		globalPassword:     config.GlobalPassword,
		globalSalt:         config.GlobalSalt,
	}
}

// normalizePath normalizes file paths for consistent database lookups
// Removes trailing slashes except for root path "/"
func normalizePath(path string) string {
	// Handle empty path
	if path == "" {
		return RootPath
	}

	// Handle root path - keep as is
	if path == RootPath {
		return path
	}

	// Remove trailing slashes for all other paths
	return strings.TrimRight(path, "/")
}
