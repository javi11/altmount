//go:build !windows

package utils

// isPlatformCrossDeviceError has no extra cases outside Windows: os.Rename reports EXDEV directly,
// which isCrossDeviceError already checks.
func isPlatformCrossDeviceError(_ error) bool {
	return false
}
