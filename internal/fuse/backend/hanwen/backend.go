package hanwen

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"github.com/javi11/altmount/internal/fuse/backend"
)

const mountTimeout = 120 * time.Second

func init() {
	backend.Register(backend.Hanwen, func(cfg backend.Config) (backend.Backend, error) {
		return New(cfg), nil
	})
}

// Backend is the hanwen/go-fuse FUSE backend.
type Backend struct {
	cfg    backend.Config
	logger *slog.Logger

	mu     sync.Mutex
	server *fuse.Server
	root   *Dir
}

// New creates a new hanwen backend.
func New(cfg backend.Config) *Backend {
	return &Backend{
		cfg:    cfg,
		logger: slog.With("component", "fuse-hanwen"),
	}
}

// Type returns the backend type.
func (b *Backend) Type() backend.Type {
	return backend.Hanwen
}

// Mount starts the FUSE filesystem. Blocks until unmount.
func (b *Backend) Mount(ctx context.Context, onReady func()) error {
	b.cleanup()

	root := NewDir(b.cfg.NzbFs, "", b.logger, b.cfg.UID, b.cfg.GID, b.cfg.StreamTracker)

	attrTimeout := time.Duration(b.cfg.FuseConfig.AttrTimeoutSeconds) * time.Second
	entryTimeout := time.Duration(b.cfg.FuseConfig.EntryTimeoutSeconds) * time.Second

	if attrTimeout == 0 {
		attrTimeout = 30 * time.Second
	}
	if entryTimeout == 0 {
		entryTimeout = 1 * time.Second
	}

	maxReadAhead := b.cfg.FuseConfig.MaxReadAheadMB * 1024 * 1024
	if maxReadAhead == 0 {
		maxReadAhead = 128 * 1024 * 1024 // 128MB default
	}

	opts := &fs.Options{
		MountOptions: fuse.MountOptions{
			AllowOther:           b.cfg.FuseConfig.AllowOther,
			Name:                 "altmount",
			Debug:                b.cfg.FuseConfig.Debug,
			MaxReadAhead:         maxReadAhead,
			MaxBackground:        64,
			DisableXAttrs:        true,
			IgnoreSecurityLabels: true,
			DisableReadDirPlus:   true,
			DirectMount:          true,
			MaxWrite:             1024 * 1024, // 1MB
		},
		EntryTimeout:    &entryTimeout,
		AttrTimeout:     &attrTimeout,
		NegativeTimeout: &entryTimeout,
	}

	// macOS-specific mount options
	if runtime.GOOS == "darwin" {
		opts.Options = append(opts.Options,
			fmt.Sprintf("volname=%s", "altmount"),
			"noapplexattr",
			"noappledouble",
		)
		opts.DirectMount = false // not supported on macOS
	}

	// Mount with timeout
	type mountResult struct {
		server *fuse.Server
		err    error
	}
	ch := make(chan mountResult, 1)

	go func() {
		s, err := fs.Mount(b.cfg.MountPoint, root, opts)
		ch <- mountResult{server: s, err: err}
	}()

	var server *fuse.Server
	select {
	case result := <-ch:
		if result.err != nil {
			return fmt.Errorf("failed to mount FUSE filesystem: %w", result.err)
		}
		server = result.server
	case <-time.After(mountTimeout):
		return fmt.Errorf("FUSE mount timed out after %s", mountTimeout)
	case <-ctx.Done():
		return ctx.Err()
	}

	b.mu.Lock()
	b.server = server
	b.root = root
	b.mu.Unlock()

	if err := server.WaitMount(); err != nil {
		b.logger.Error("WaitMount failed, unmounting", "error", err)
		_ = server.Unmount()
		return fmt.Errorf("FUSE mount not ready: %w", err)
	}

	b.logger.Info("FUSE filesystem mounted and ready", "mountpoint", b.cfg.MountPoint)

	if onReady != nil {
		onReady()
	}

	server.Wait()
	return nil
}

// Unmount gracefully unmounts the filesystem.
func (b *Backend) Unmount() error {
	b.mu.Lock()
	server := b.server
	b.mu.Unlock()

	b.logger.Info("Unmounting FUSE filesystem", "mountpoint", b.cfg.MountPoint)

	if server != nil {
		err := server.Unmount()
		if err == nil {
			return nil
		}
		b.logger.Warn("Standard unmount failed, attempting force unmount", "error", err)
	}

	return b.ForceUnmount()
}

// ForceUnmount attempts to lazy/force unmount using platform-specific commands.
func (b *Backend) ForceUnmount() error {
	var methods [][]string

	if runtime.GOOS == "darwin" {
		methods = [][]string{
			{"umount", "-f", b.cfg.MountPoint},
			{"diskutil", "unmount", "force", b.cfg.MountPoint},
			{"umount", b.cfg.MountPoint},
		}
	} else {
		methods = [][]string{
			{"fusermount", "-uz", b.cfg.MountPoint},
			{"umount", b.cfg.MountPoint},
			{"umount", "-l", b.cfg.MountPoint},
			{"fusermount3", "-uz", b.cfg.MountPoint},
		}
	}

	for _, method := range methods {
		if err := exec.Command(method[0], method[1:]...).Run(); err == nil {
			b.logger.Info("Successfully force unmounted", "command", method, "path", b.cfg.MountPoint)
			return nil
		}
	}

	return fmt.Errorf("all force unmount attempts failed for %s", b.cfg.MountPoint)
}

// RefreshDirectory invalidates the kernel cache for a named directory entry.
func (b *Backend) RefreshDirectory(name string) {
	b.mu.Lock()
	root := b.root
	b.mu.Unlock()

	if root == nil {
		return
	}

	if name == "" || name == "/" {
		root.Refresh()
		return
	}

	root.RefreshChild(name)
}

// cleanup attempts to clean stale mounts before mounting.
func (b *Backend) cleanup() {
	_ = b.ForceUnmount()
}
