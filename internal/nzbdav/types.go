package nzbdav

import (
	"io"
	"time"
)

type Segment struct {
	Number int    `json:"number"`
	MsgID  string `json:"msgid"`
	Bytes  int64  `json:"bytes"`
}

type FileEntry struct {
	Subject  string    `json:"subject"`
	Date     time.Time `json:"date"`
	Segments []Segment `json:"segments"`
}

type Release struct {
	Name     string
	Category string
	Files    []FileEntry
}

type ParsedNzb struct {
	Category string
	Name     string
	RelPath  string
	Content  io.Reader
}
