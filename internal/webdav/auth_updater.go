package webdav

import (
	"fmt"
	"log/slog"
	"sync"
)

// AuthCredentials holds the current WebDAV authentication credentials
type AuthCredentials struct {
	mu       sync.RWMutex
	username string
	password string
}

// NewAuthCredentials creates new authentication credentials
func NewAuthCredentials(username, password string) *AuthCredentials {
	return &AuthCredentials{
		username: username,
		password: password,
	}
}

// GetCredentials returns the current credentials (thread-safe)
func (ac *AuthCredentials) GetCredentials() (string, string) {
	ac.mu.RLock()
	defer ac.mu.RUnlock()
	return ac.username, ac.password
}

// UpdateCredentials updates the credentials (thread-safe)
func (ac *AuthCredentials) UpdateCredentials(username, password string) {
	ac.mu.Lock()
	defer ac.mu.Unlock()
	ac.username = username
	ac.password = password
}

// AuthUpdater provides methods to update WebDAV authentication
type AuthUpdater struct {
	credentials *AuthCredentials
	logger      *slog.Logger
}

// NewAuthUpdater creates a new WebDAV auth updater
func NewAuthUpdater() *AuthUpdater {
	return &AuthUpdater{
		logger: slog.Default(),
	}
}

// SetAuthCredentials sets the auth credentials reference for dynamic updates
func (u *AuthUpdater) SetAuthCredentials(credentials *AuthCredentials) {
	u.credentials = credentials
}

// UpdateAuth updates WebDAV authentication credentials
func (u *AuthUpdater) UpdateAuth(username, password string) error {
	if u.credentials == nil {
		return fmt.Errorf("auth credentials not initialized")
	}

	u.logger.Info("Updating WebDAV authentication credentials", "username", username)
	u.credentials.UpdateCredentials(username, password)
	u.logger.Info("WebDAV authentication credentials updated successfully")

	return nil
}
