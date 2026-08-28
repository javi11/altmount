package rclonecli

import (
	"os"
	"strings"
)

// toSlashSep rewrites sep to '/' in s. Split out from ToVFSPath so the Windows
// behaviour can be exercised on any platform: filepath.ToSlash is a no-op when
// the OS separator is already '/', which would otherwise leave the separator
// handling untested on a Linux CI runner.
func toSlashSep(s string, sep byte) string {
	if sep == '/' {
		return s
	}
	return strings.ReplaceAll(s, string(sep), "/")
}

// ToVFSPath converts a directory into the separator form rclone's VFS expects.
//
// rclone's VFS is forward-slash on every platform, so a Windows-separated path
// like `tv\Show` matches no node. The failure is silent: vfs/forget echoes back
// whatever it is given and reports success even for a directory that does not
// exist, so a caller sees "Successfully notified" while nothing was invalidated.
//
// On POSIX this is a no-op: '/' is already the separator, and a backslash is a
// legal character in a POSIX filename that must be left alone.
func ToVFSPath(dir string) string {
	return toSlashSep(dir, os.PathSeparator)
}

// ToVFSPaths applies ToVFSPath to every element, returning a new slice.
//
// Used at the RC boundary so every caller is covered, including ones added
// later. Callers should still build correct virtual paths - normalizing here is
// a backstop, not a licence to pass OS paths - but a missed site degrades to a
// working call instead of a silent no-op.
func ToVFSPaths(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		out = append(out, ToVFSPath(d))
	}
	return out
}
