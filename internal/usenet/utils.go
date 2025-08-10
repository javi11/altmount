package usenet

import (
	"mime"
	"path/filepath"
	"strings"
)

const (
	NzbExtension  = ".nzb"
	StrmExtension = ".strm"
	Par2Extension = ".par2"
)

// Only working for new files and format
func GetRealFileExtension(name string) string {
	return filepath.Ext(strings.TrimSuffix(name, filepath.Ext(name)))
}

func RemoveMetadataExtension(name string, trueExtension string) string {
	n := name
	if strings.HasSuffix(name, NzbExtension) ||
		strings.HasSuffix(name, StrmExtension) {
		n = strings.TrimSuffix(name, filepath.Ext(name))
	}

	fExt := filepath.Ext(n)
	// Check if the file has a valid extension
	t := mime.TypeByExtension(fExt)
	if t != "" || fExt == trueExtension {
		return n
	}

	// Maintain compatibility with nzb files created outside the system
	return n + trueExtension
}

func ReplaceFileExtension(name string, extension string) string {
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + extension
}

func AddNzbExtension(name string) string {
	return name + NzbExtension
}

func AddStrmExtension(name string) string {
	return name + StrmExtension
}
