package utils

import (
	"path"
	"path/filepath"
	"strings"
)

func ToAbsPath(name string) string {
	if name == "" {
		return string(filepath.Separator)
	}

	if !filepath.IsAbs(name) {
		name = string(filepath.Separator) + name
	}

	return filepath.Clean(filepath.FromSlash(name))
}

func SlashClean(name string) string {
	return path.Clean(name)
}

func RemoveRootPath(rootPath, path string) string {
	if rootPath == "" {
		return path
	}
	return strings.TrimPrefix(path, rootPath)
}

func WindowsPathToLinux(path string) string {
	return strings.ReplaceAll(path, "\\", "/")
}
