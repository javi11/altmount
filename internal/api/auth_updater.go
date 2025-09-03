package api

import "sync"

// AuthUpdater provides methods to update API authentication
type AuthUpdater struct {
	server *Server
	mutex  sync.RWMutex
}

// NewAuthUpdater creates a new API auth updater
func NewAuthUpdater(server *Server) *AuthUpdater {
	return &AuthUpdater{
		server: server,
	}
}

// UpdateAuth updates API authentication credentials (OAuth only)
func (u *AuthUpdater) UpdateAuth(username, password string) error {
	u.mutex.Lock()
	defer u.mutex.Unlock()

	// Basic authentication has been removed - OAuth flow handles authentication
	// This method is kept for interface compatibility but does nothing
	_ = username
	_ = password

	return nil
}
