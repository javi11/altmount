package nzbdav

import (
	"io"
)

type ParsedNzb struct {
	Category string
	Name     string
	RelPath  string
	Content  io.Reader
}
