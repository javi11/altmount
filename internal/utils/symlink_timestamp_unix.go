//go:build !windows

package utils

import (
	"time"

	"golang.org/x/sys/unix"
)

func setSymlinkTimes(path string, t time.Time) error {
	ns := t.UnixNano()
	times := []unix.Timespec{unix.NsecToTimespec(ns), unix.NsecToTimespec(ns)}
	return unix.UtimesNanoAt(unix.AT_FDCWD, path, times, unix.AT_SYMLINK_NOFOLLOW)
}
