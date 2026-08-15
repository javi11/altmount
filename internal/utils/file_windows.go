//go:build windows

package utils

import (
	"errors"
	"syscall"
)

// errorNotSameDevice is the Win32 ERROR_NOT_SAME_DEVICE code that os.Rename returns when source
// and destination live on different drives. Windows has no real EXDEV: syscall.EXDEV there is an
// invented errno from the APPLICATION_ERROR block, so it never matches this, and the Windows
// message ("The system cannot move the file to a different disk drive.") never says "cross-device".
const errorNotSameDevice = syscall.Errno(17)

func isPlatformCrossDeviceError(err error) bool {
	return errors.Is(err, errorNotSameDevice)
}
