package utils

import (
	"golang.org/x/sys/unix"
)

// DiskSpace holds information about disk space usage
type DiskSpace struct {
	Total int64
	Free  int64
	Used  int64
}

// GetDiskSpace returns disk space information for the given path
func GetDiskSpace(path string) (DiskSpace, error) {
	var stat unix.Statfs_t
	if err := unix.Statfs(path, &stat); err != nil {
		return DiskSpace{}, err
	}

	// Calculate bytes
	total := int64(stat.Blocks) * int64(stat.Bsize)
	free := int64(stat.Bfree) * int64(stat.Bsize)
	used := total - free

	return DiskSpace{
		Total: total,
		Free:  free,
		Used:  used,
	}, nil
}
