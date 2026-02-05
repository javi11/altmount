package model

import (
	"fmt"

	"github.com/javi11/altmount/internal/config"
)

var (
	ErrPathMatchFailed           = fmt.Errorf("path match failed")
	ErrEpisodeAlreadySatisfied   = fmt.Errorf("episode already satisfied by another file")
	ErrInstanceNotFound          = fmt.Errorf("instance not found")
)

// ConfigInstance represents an arrs instance from configuration
type ConfigInstance struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // "radarr" or "sonarr"
	URL      string `json:"url"`
	APIKey   string `json:"api_key"`
	Category string `json:"category"`
	Enabled  bool   `json:"enabled"`
}

// ConfigManager interface defines methods needed for configuration management
type ConfigManager interface {
	GetConfig() *config.Config
	UpdateConfig(config *config.Config) error
	SaveConfig() error
}
