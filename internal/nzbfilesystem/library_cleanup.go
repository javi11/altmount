package nzbfilesystem

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// canCleanLibrary prevents physical cleanup from re-entering our own mount
// while a DELETE may hold a FUSE directory lock. Virtual directories are already
// removed by DeleteFileMetadata. Failure to resolve a path skips optional cleanup.
func canCleanLibrary(root, cleanupDir, mount string) bool {
	if root == "" {
		return false
	}
	root, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	cleanupDir, err = filepath.Abs(cleanupDir)
	if err != nil || !pathWithin(cleanupDir, root) {
		return false
	}
	if mount == "" {
		return true
	}
	mount, err = filepath.Abs(mount)
	if err != nil || pathWithin(root, mount) {
		return false
	}

	// Resolve aliases of the configured boundary without entering its children.
	mount, ok := resolveMountBoundary(mount)
	if !ok {
		return false
	}
	resolvedRoot, safe := resolveLibraryOutsideMount(root, mount, os.Lstat)
	if !safe {
		return false
	}
	// A child of an external library may itself be a symlink into the mount.
	// Check the actual cleanup directory before RemoveEmptyDirs touches it.
	resolvedDir, safe := resolveLibraryOutsideMount(cleanupDir, mount, os.Lstat)
	return safe && pathWithin(resolvedDir, resolvedRoot)
}

// Readlink inspects the boundary itself, so a configured symlink to the real
// mount is recognized. EINVAL means the boundary is not a symlink. Never resolve
// library paths this way: their targets may be virtual directories on the mount.
func resolveMountBoundary(mount string) (string, bool) {
	for links := 0; links < 255; links++ {
		parent, err := filepath.EvalSymlinks(filepath.Dir(mount))
		if err != nil {
			return "", false
		}
		mount = filepath.Join(parent, filepath.Base(mount))
		target, err := os.Readlink(mount)
		if errors.Is(err, syscall.EINVAL) {
			return mount, true
		}
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(parent, target)
		}
		mount = target
	}
	return "", false
}

func pathWithin(path, mount string) bool {
	return path == mount || strings.HasPrefix(path, mount+string(filepath.Separator)) || mount == string(filepath.Separator)
}

// resolveLibraryOutsideMount follows symlinks one component at a time, stopping
// before any lookup on the mount. EvalSymlinks(root) would follow an external
// library symlink into the mount and could deadlock before cleanup even starts.
func resolveLibraryOutsideMount(root, mount string, lstat func(string) (os.FileInfo, error)) (string, bool) {
	volume := filepath.VolumeName(root)
	current := volume + string(filepath.Separator)
	pending := strings.Split(strings.TrimPrefix(root, current), string(filepath.Separator))
	links := 0
	for len(pending) > 0 {
		component := pending[0]
		pending = pending[1:]
		if component == "" || component == "." {
			continue
		}
		current = filepath.Join(current, component)
		if pathWithin(current, mount) {
			return "", false
		}
		info, err := lstat(current)
		if err != nil {
			return "", false
		}
		if info.Mode()&os.ModeSymlink == 0 {
			continue
		}
		links++
		if links > 255 {
			return "", false
		}
		target, err := os.Readlink(current)
		if err != nil {
			return "", false
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		if pathWithin(filepath.Clean(target), mount) {
			return "", false
		}
		volume = filepath.VolumeName(target)
		current = volume + string(filepath.Separator)
		pending = append(strings.Split(strings.TrimPrefix(target, current), string(filepath.Separator)), pending...)
	}
	return current, true
}
