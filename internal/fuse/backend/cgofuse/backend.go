package cgofuse

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"runtime"
	"sync"

	cgofuse "github.com/winfsp/cgofuse/fuse"

	"github.com/javi11/altmount/internal/fuse/backend"
)

func init() {
	backend.Register(backend.Cgo, func(cfg backend.Config) (backend.Backend, error) {
		return New(cfg), nil
	})
}

// Backend is the cgofuse FUSE backend (macOS via Fuse-T, Windows via WinFsp).
type Backend struct {
	cfg    backend.Config
	logger *slog.Logger

	mu   sync.Mutex
	host *cgofuse.FileSystemHost
	done chan struct{}
}

// New creates a new cgofuse backend.
func New(cfg backend.Config) *Backend {
	return &Backend{
		cfg:    cfg,
		logger: slog.With("component", "fuse-cgofuse"),
	}
}

// Type returns the backend type.
func (b *Backend) Type() backend.Type {
	return backend.Cgo
}

// Mount starts the FUSE filesystem. Blocks until unmount.
func (b *Backend) Mount(ctx context.Context, onReady func()) error {
	b.cleanup()

	filesystem := NewFS(b.cfg, b.logger)
	host := cgofuse.NewFileSystemHost(filesystem)

	b.mu.Lock()
	b.host = host
	b.done = make(chan struct{})
	b.mu.Unlock()

	opts := b.mountOptions()

	b.logger.Info("Mounting cgofuse FUSE filesystem",
		"mountpoint", b.cfg.MountPoint,
		"options", opts)

	// Mount in a goroutine — host.Mount blocks
	go func() {
		defer close(b.done)
		if !host.Mount(b.cfg.MountPoint, opts) {
			b.logger.Error("cgofuse mount returned false", "mountpoint", b.cfg.MountPoint)
		}
	}()

	// Wait for Init callback to fire (filesystem sets ready)
	select {
	case <-filesystem.Ready():
		b.logger.Info("cgofuse FUSE filesystem mounted and ready", "mountpoint", b.cfg.MountPoint)
	case <-b.done:
		return fmt.Errorf("cgofuse mount failed (exited before ready)")
	case <-ctx.Done():
		host.Unmount()
		return ctx.Err()
	}

	if onReady != nil {
		onReady()
	}

	// Block until unmount
	select {
	case <-b.done:
	case <-ctx.Done():
		host.Unmount()
		<-b.done
	}

	return nil
}

// Unmount gracefully unmounts the filesystem.
func (b *Backend) Unmount() error {
	b.mu.Lock()
	host := b.host
	b.mu.Unlock()

	b.logger.Info("Unmounting cgofuse FUSE filesystem", "mountpoint", b.cfg.MountPoint)

	if host != nil {
		host.Unmount()
		return nil
	}

	return b.ForceUnmount()
}

// ForceUnmount attempts platform-specific force unmount.
func (b *Backend) ForceUnmount() error {
	var methods [][]string

	switch runtime.GOOS {
	case "darwin":
		methods = [][]string{
			{"umount", "-f", b.cfg.MountPoint},
			{"diskutil", "unmount", "force", b.cfg.MountPoint},
			{"umount", b.cfg.MountPoint},
		}
	case "windows":
		// WinFsp doesn't need force unmount; host.Unmount() suffices
		return nil
	default:
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

// mountOptions returns platform-specific mount options.
func (b *Backend) mountOptions() []string {
	var opts []string

	switch runtime.GOOS {
	case "darwin":
		opts = append(opts,
			"-o", "volname=altmount",
			"-o", "noapplexattr",
			"-o", "noappledouble",
		)
		if b.cfg.FuseConfig.AllowOther {
			opts = append(opts, "-o", "allow_other")
		}
	case "windows":
		// WinFsp options
		opts = append(opts,
			fmt.Sprintf("--VolumePrefix=%s", "altmount"),
			"-o", "uid=-1",
			"-o", "gid=-1",
		)
	default:
		if b.cfg.FuseConfig.AllowOther {
			opts = append(opts, "-o", "allow_other")
		}
	}

	return opts
}

// cleanup attempts to clean stale mounts before mounting.
func (b *Backend) cleanup() {
	_ = b.ForceUnmount()
}
