package webdav

import (
	"context"
	"log/slog"

	"github.com/javi11/altmount/internal/config"
)

type Config struct {
	// Port is the port where the webdav server will be listening
	Port int `yaml:"port" default:"8080" mapstructure:"port"`
	// User is the user to access the webdav server
	User string `yaml:"username" default:"usenet" json:"-" mapstructure:"username"`
	// Pass is the password to access the webdav server
	Pass string `yaml:"password" default:"usenet" json:"-" mapstructure:"password"`
	// Prefix is the URL path prefix for the WebDAV server
	Prefix string `yaml:"prefix" default:"/webdav/" mapstructure:"prefix"`
}

// RegisterConfigHandlers registers handlers for WebDAV-related configuration changes
func RegisterConfigHandlers(ctx context.Context, configManager *config.Manager, handler *Handler) {
	configManager.OnConfigChange(func(oldConfig, newConfig *config.Config) {
		// Sync WebDAV auth credentials if they changed
		if oldConfig.WebDAV.User != newConfig.WebDAV.User || oldConfig.WebDAV.Password != newConfig.WebDAV.Password {
			handler.SyncAuthCredentials()
			slog.InfoContext(ctx, "WebDAV auth credentials updated",
				"old_user", oldConfig.WebDAV.User,
				"new_user", newConfig.WebDAV.User)
		}
	})
}
