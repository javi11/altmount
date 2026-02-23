package backend

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/nzbfilesystem"
)

// Type identifies a FUSE backend implementation.
type Type string

const (
	Hanwen Type = "hanwen"
	Cgo    Type = "cgo"
)

// StreamTracker is the subset of stream tracking needed by FUSE backends.
type StreamTracker interface {
	AddStream(filePath, source, userName, clientIP, userAgent string, totalSize int64) *nzbfilesystem.ActiveStream
	UpdateProgress(id string, bytesRead int64)
	Remove(id string)
}

// Backend abstracts FUSE mount/unmount operations.
type Backend interface {
	// Mount starts the FUSE filesystem. Blocks until unmount.
	// onReady is called once the kernel mount is confirmed live.
	Mount(ctx context.Context, onReady func()) error

	// Unmount gracefully unmounts the filesystem.
	Unmount() error

	// ForceUnmount attempts platform-specific force unmount.
	ForceUnmount() error

	// Type returns the backend type.
	Type() Type
}

// Refresher is optionally implemented by backends that support
// kernel cache invalidation (e.g. hanwen via NotifyContent/NotifyEntry).
type Refresher interface {
	RefreshDirectory(name string)
}

// Config holds parameters common to all backends.
type Config struct {
	MountPoint    string
	NzbFs         *nzbfilesystem.NzbFilesystem
	FuseConfig    config.FuseConfig
	StreamTracker StreamTracker
	UID           uint32
	GID           uint32
}

// Factory creates a Backend from a Config.
type Factory func(cfg Config) (Backend, error)

var (
	mu        sync.RWMutex
	factories = make(map[Type]Factory)
)

// Register registers a backend factory for the given type.
func Register(t Type, f Factory) {
	mu.Lock()
	defer mu.Unlock()
	factories[t] = f
}

// Create creates a backend of the given type.
func Create(t Type, cfg Config) (Backend, error) {
	mu.RLock()
	f, ok := factories[t]
	mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown FUSE backend type: %s", t)
	}
	return f(cfg)
}

// DefaultType returns the platform-default backend type.
// Linux uses hanwen (pure Go, fastest). macOS/Windows use cgo (Fuse-T/WinFsp).
// Override with ALTMOUNT_FUSE_BACKEND env var (checked by caller).
func DefaultType() Type {
	switch runtime.GOOS {
	case "linux":
		return Hanwen
	default:
		return Cgo
	}
}
