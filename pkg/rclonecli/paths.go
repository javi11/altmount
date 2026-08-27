package rclonecli

import "path/filepath"

// ToVFSPath converts a directory into the separator form rclone's VFS expects.
//
// rclone's VFS is forward-slash on every platform, so a Windows-separated path
// like `tv\Show` matches no node. The failure is silent: vfs/forget echoes back
// whatever it is given and reports success even for a directory that does not
// exist, so a caller sees "Successfully notified" while nothing was invalidated.
//
// This runs at the RC boundary so every caller is covered, including ones added
// later. Callers should still build correct virtual paths - normalizing here is
// a backstop, not a licence to pass OS paths - but a missed site degrades to a
// working call instead of a silent no-op.
//
// On POSIX this is a no-op: filepath.ToSlash only rewrites when the OS
// separator is not '/', so a backslash (a legal character in a POSIX filename)
// is left alone.
func ToVFSPath(dir string) string {
	return filepath.ToSlash(dir)
}
