package nzbdav

import (
	"io"
)

type ParsedNzb struct {
	ID             string
	Category       string
	Name           string
	RelPath        string
	Content        io.Reader
	ExtractedFiles []ExtractedFileInfo
}

type ExtractedFileInfo struct {
	Name string
	Size int64
}
