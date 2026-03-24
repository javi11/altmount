//go:build windows

package api

// getDiskSpace returns free and total disk space in bytes for the given path.
// Not implemented for Windows cross-compiled builds.
func getDiskSpace(_ string) (free, total uint64) {
	return 0, 0
}
