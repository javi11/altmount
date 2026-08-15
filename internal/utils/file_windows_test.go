//go:build windows

package utils

import (
	"os"
	"syscall"
	"testing"
)

// os.Rename on Windows fails a cross-drive move with ERROR_NOT_SAME_DEVICE, which is a raw
// Win32 code and not syscall.EXDEV (an invented errno on Windows), so it needs its own check.
func TestIsCrossDeviceError_WindowsNotSameDevice(t *testing.T) {
	err := &os.LinkError{Op: "rename", Old: "a", New: "b", Err: errorNotSameDevice}

	if !isCrossDeviceError(err) {
		t.Errorf("isCrossDeviceError(%v) = false; want true", err)
	}

	if errorNotSameDevice != syscall.Errno(17) {
		t.Errorf("errorNotSameDevice = %d; want 17 (ERROR_NOT_SAME_DEVICE)", errorNotSameDevice)
	}
}
