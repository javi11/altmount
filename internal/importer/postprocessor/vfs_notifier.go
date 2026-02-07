package postprocessor

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/pathutil"
)

// NotifyVFS notifies rclone VFS about file changes
func (c *Coordinator) NotifyVFS(ctx context.Context, resultingPath string, async bool) {
	if c.rcloneClient == nil {
		return
	}

	refreshFunc := func(path string) {
		refreshCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()

		cfg := c.configGetter()
		vfsName := cfg.RClone.VFSName
		if vfsName == "" {
			vfsName = config.MountProvider
		}

		// Normalize paths for rclone (no leading slash)
		normalizeForRclone := func(p string) string {
			p = strings.TrimPrefix(p, "/")
			if p == "" {
				return "."
			}
			return p
		}

		dirsToRefresh := []string{normalizeForRclone(path)}
		parentDir := filepath.Dir(path)
		if parentDir != "." && parentDir != "/" {
			dirsToRefresh = append(dirsToRefresh, normalizeForRclone(parentDir))

			// Also refresh grandparent if parent might be new
			grandParent := filepath.Dir(parentDir)
			if grandParent != "." && grandParent != "/" {
				dirsToRefresh = append(dirsToRefresh, normalizeForRclone(grandParent))
			}
		}

		slog.DebugContext(refreshCtx, "Notifying rclone VFS refresh", "dirs", dirsToRefresh, "vfs", vfsName)

		err := c.rcloneClient.RefreshDir(refreshCtx, vfsName, dirsToRefresh)
		if err != nil {
			slog.WarnContext(refreshCtx, "Failed to notify rclone VFS refresh",
				"dirs", dirsToRefresh,
				"error", err)
		} else {
			slog.InfoContext(refreshCtx, "Successfully notified rclone VFS refresh",
				"dirs", dirsToRefresh)
		}
	}

	if async {
		go refreshFunc(resultingPath)
	} else {
		refreshFunc(resultingPath)
	}
}

// RefreshMountPathIfNeeded refreshes the mount path cache if required
func (c *Coordinator) RefreshMountPathIfNeeded(ctx context.Context, resultingPath string, itemID int64) {
	if c.rcloneClient == nil {
		return
	}

	cfg := c.configGetter()
	mountPath := pathutil.JoinAbsPath(cfg.MountPath, filepath.Dir(resultingPath))

	if _, err := os.Stat(mountPath); err != nil {
		if os.IsNotExist(err) {
			vfsName := cfg.RClone.VFSName
			if vfsName == "" {
				vfsName = config.MountProvider
			}

			// Refresh the root path if the mount path is not found
			if err := c.rcloneClient.RefreshDir(ctx, vfsName, []string{"/"}); err != nil {
				c.log.ErrorContext(ctx, "Failed to refresh mount path",
					"queue_id", itemID,
					"path", mountPath,
					"error", err)
			}
		}
	}
}
