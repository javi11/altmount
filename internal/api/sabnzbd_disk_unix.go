//go:build !windows

package api

import "syscall"

// getDiskSpace returns free and total disk space in bytes for the given path.
func getDiskSpace(path string) (free, total uint64) {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err == nil {
		free = stat.Bavail * uint64(stat.Bsize)
		total = stat.Blocks * uint64(stat.Bsize)
	}
	return
}
