package utils

import (
	"fmt"
	"time"
)

// PinSymlinkTime sets the modification and access time of a symlink to the
// given RFC3339 timestamp. This is used to give all library symlinks a fixed,
// stable timestamp so media servers (e.g. Plex) never treat them as new or
// modified after a regeneration. If timestamp is empty the function is a no-op.
func PinSymlinkTime(symlinkPath, timestamp string) error {
	if timestamp == "" {
		return nil
	}

	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return fmt.Errorf("invalid pin_symlink_timestamp value %q: %w", timestamp, err)
	}

	if err := setSymlinkTimes(symlinkPath, t); err != nil {
		return fmt.Errorf("failed to set timestamp on %s: %w", symlinkPath, err)
	}

	return nil
}
