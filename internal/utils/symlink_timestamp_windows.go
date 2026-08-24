//go:build windows

package utils

import (
	"fmt"
	"time"
)

// Refuse rather than silently changing the target file. Windows has no
// portable no-follow timestamp operation in the Go standard library.
func setSymlinkTimes(_ string, _ time.Time) error {
	return fmt.Errorf("pinning symlink timestamps is not supported on Windows")
}
