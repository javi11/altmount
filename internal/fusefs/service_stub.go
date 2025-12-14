//go:build !fuse

package fusefs

import (
	"context"
	"log/slog"

	"github.com/javi11/altmount/internal/config"
)

// FuseService manages the lifecycle of the FUSE mount (Stub)
type FuseService struct {
}

// NewFuseService creates a new FuseService (Stub)
func NewFuseService(configManager *config.Manager) *FuseService {
	return &FuseService{}
}

// Start starts the FUSE mount (Stub)
func (s *FuseService) Start(ctx context.Context) error {
	slog.Info("FUSE support is not enabled in this build. Use -tags fuse to enable.")
	return nil
}

// Stop stops the FUSE mount (Stub)
func (s *FuseService) Stop(ctx context.Context) error {
	return nil
}

// RegisterConfigHandlers registers callbacks for configuration changes (Stub)
func (s *FuseService) RegisterConfigHandlers(configManager *config.Manager) {
	// No-op
}
