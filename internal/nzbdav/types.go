package nzbdav

import (
	"io"
)

// ParsedNzbAlias represents a DavItem that shares the same NZB blob as the
// canonical ParsedNzb but refers to a different virtual output file (typically
// a different episode within the same season-pack NZB).
type ParsedNzbAlias struct {
	ID   string // DavItem.Id
	Name string // DavItem.Name (episode filename)
}

type ParsedNzb struct {
	ID             string
	Category       string
	Name           string
	RelPath        string
	Content        io.Reader
	ExtractedFiles []ExtractedFileInfo
	// DavItemName is the DavItem.Name for the canonical DavItem (may be empty for
	// legacy parseLegacy items or when Name is unavailable in the DB).
	DavItemName string
	// AliasDavItems contains other DavItems that share the same NZB blob.
	// Non-empty only for multi-file season-pack blobs where each DavItem
	// represents a distinct episode file.
	AliasDavItems []ParsedNzbAlias
}

type ExtractedFileInfo struct {
	Name string
	Size int64
}
