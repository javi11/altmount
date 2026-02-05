package parser

import (
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// NzbType represents the type of NZB content
type NzbType string

const (
	NzbTypeSingleFile NzbType = "single_file"
	NzbTypeMultiFile  NzbType = "multi_file"
	NzbTypeRarArchive NzbType = "rar_archive"
	NzbType7zArchive  NzbType = "7z_archive"
	NzbTypeStrm       NzbType = "strm_file"
)

type ExtractedFileInfo struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
}

// ParsedNzb contains the parsed NZB data and extracted metadata
type ParsedNzb struct {
	Path           string
	Filename       string
	TotalSize      int64
	Type           NzbType
	Files          []ParsedFile
	SegmentsCount  int
	password       string // Private field - use GetPassword() to access
	ExtractedFiles []ExtractedFileInfo
}

// GetPassword returns the password for this NZB
func (p *ParsedNzb) GetPassword() string {
	return p.password
}

// SetPassword sets the password for this NZB
func (p *ParsedNzb) SetPassword(password string) {
	p.password = password
}

// ParsedFile represents a file extracted from the NZB
type ParsedFile struct {
	Subject       string
	Filename      string
	Size          int64
	Segments      []*metapb.SegmentData
	Groups        []string
	IsRarArchive  bool
	Is7zArchive   bool
	IsPar2Archive bool
	Encryption    metapb.Encryption // Encryption type (e.g., "rclone"), nil if not encrypted
	Password      string            // Password from NZB meta, nil if not encrypted
	Salt          string            // Salt from NZB meta, nil if not encrypted
	ReleaseDate   time.Time         // Release date from the Usenet post
	OriginalIndex int               // Original position in the parsed NZB file list
	NzbdavID      string            // Original ID from nzbdav (for backward compatibility)
	AesKey        []byte            // AES encryption key (for nzbdav compatibility)
	AesIv         []byte            // AES initialization vector (for nzbdav compatibility)
}
