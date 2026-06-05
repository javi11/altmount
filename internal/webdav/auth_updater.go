package webdav

import (
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
