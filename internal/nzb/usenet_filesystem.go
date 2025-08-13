package nzb

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/nntppool"
	"github.com/javi11/nzbparser"
)

// Compile-time interface checks
var (
	_ fs.File   = (*UsenetFile)(nil)       // UsenetFile implements fs.File
	_ io.Seeker = (*UsenetFile)(nil)       // UsenetFile implements io.Seeker
	_ fs.FS     = (*UsenetFileSystem)(nil) // UsenetFileSystem implements fs.FS
)

// UsenetFileSystem implements fs.FS for reading RAR archives from Usenet
// This allows rardecode.OpenReader to access multi-part RAR files without downloading them entirely
type UsenetFileSystem struct {
	ctx        context.Context
	cp         nntppool.UsenetConnectionPool
	nzbFile    *database.NzbFile
	files      []ParsedFile
	maxWorkers int
}

// UsenetFile implements fs.File and io.Seeker for reading individual RAR parts from Usenet
// The Seeker interface allows rardecode.OpenReader to efficiently seek within RAR parts
type UsenetFile struct {
	name       string
	file       *ParsedFile
	nzbFile    *database.NzbFile
	cp         nntppool.UsenetConnectionPool
	ctx        context.Context
	maxWorkers int
	size       int64
	reader     io.ReadCloser
	position   int64
	closed     bool
}

// UsenetFileInfo implements fs.FileInfo for RAR part files
type UsenetFileInfo struct {
	name string
	size int64
}

// NewUsenetFileSystem creates a new filesystem for accessing RAR parts from Usenet
func NewUsenetFileSystem(ctx context.Context, cp nntppool.UsenetConnectionPool, nzbFile *database.NzbFile, files []ParsedFile, maxWorkers int) *UsenetFileSystem {
	return &UsenetFileSystem{
		ctx:        ctx,
		cp:         cp,
		nzbFile:    nzbFile,
		files:      files,
		maxWorkers: maxWorkers,
	}
}

// Open opens a file in the Usenet filesystem
func (ufs *UsenetFileSystem) Open(name string) (fs.File, error) {
	name = path.Clean(name)

	// Find the corresponding RAR file
	for _, file := range ufs.files {
		if file.Filename == name || path.Base(file.Filename) == name {
			return &UsenetFile{
				name:       name,
				file:       &file,
				nzbFile:    ufs.nzbFile,
				cp:         ufs.cp,
				ctx:        ufs.ctx,
				maxWorkers: ufs.maxWorkers,
				size:       file.Size,
				position:   0,
				closed:     false,
			}, nil
		}
	}

	return nil, &fs.PathError{
		Op:   "open",
		Path: name,
		Err:  fs.ErrNotExist,
	}
}

// GetRarFiles returns all RAR-related files sorted for proper multi-part reading
func (ufs *UsenetFileSystem) GetRarFiles() []string {
	var rarFiles []string

	for _, file := range ufs.files {
		if file.IsRarArchive {
			rarFiles = append(rarFiles, file.Filename)
		}
	}

	// Sort RAR files to ensure proper order (.rar, .r00, .r01 or .part001.rar, .part002.rar)
	sort.Slice(rarFiles, func(i, j int) bool {
		return compareRarFilenames(rarFiles[i], rarFiles[j])
	})

	return rarFiles
}

// compareRarFilenames compares RAR filenames for proper sorting
func compareRarFilenames(a, b string) bool {
	// Extract base names and extensions
	aBase, aExt := splitRarFilename(a)
	bBase, bExt := splitRarFilename(b)

	// If different base names, use lexical order
	if aBase != bBase {
		return aBase < bBase
	}

	// Same base name, sort by extension/part number
	aNum := extractRarPartNumber(aExt)
	bNum := extractRarPartNumber(bExt)

	return aNum < bNum
}

// splitRarFilename splits a RAR filename into base and extension parts
func splitRarFilename(filename string) (base, ext string) {
	// Handle patterns like .part001.rar, .part01.rar
	partPattern := regexp.MustCompile(`^(.+)\.part\d+\.rar$`)
	if matches := partPattern.FindStringSubmatch(filename); len(matches) > 1 {
		return matches[1], strings.TrimPrefix(filename, matches[1]+".")
	}

	// Handle patterns like .rar, .r00, .r01
	if strings.HasSuffix(strings.ToLower(filename), ".rar") {
		return strings.TrimSuffix(filename, filepath.Ext(filename)), "rar"
	}

	rPattern := regexp.MustCompile(`^(.+)\.r(\d+)$`)
	if matches := rPattern.FindStringSubmatch(filename); len(matches) > 2 {
		return matches[1], "r" + matches[2]
	}

	return filename, ""
}

// extractRarPartNumber extracts numeric part from RAR extension for sorting
func extractRarPartNumber(ext string) int {
	// .rar is always first (part 0)
	if ext == "rar" {
		return 0
	}

	// Extract number from .r00, .r01, etc.
	rPattern := regexp.MustCompile(`^r(\d+)$`)
	if matches := rPattern.FindStringSubmatch(ext); len(matches) > 1 {
		if num := parseInt(matches[1]); num >= 0 {
			return num + 1 // .r00 becomes 1, .r01 becomes 2, etc.
		}
	}

	// Extract number from .part001.rar, .part01.rar, etc.
	partPattern := regexp.MustCompile(`^part(\d+)\.rar$`)
	if matches := partPattern.FindStringSubmatch(ext); len(matches) > 1 {
		if num := parseInt(matches[1]); num >= 0 {
			return num
		}
	}

	return 999999 // Unknown format goes last
}

// parseInt safely converts string to int
func parseInt(s string) int {
	num := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num = num*10 + int(r-'0')
		} else {
			return -1
		}
	}
	return num
}

// UsenetFile methods implementing fs.File interface

func (uf *UsenetFile) Stat() (fs.FileInfo, error) {
	return &UsenetFileInfo{
		name: uf.name,
		size: uf.size,
	}, nil
}

func (uf *UsenetFile) Read(p []byte) (n int, err error) {
	if uf.closed {
		return 0, fs.ErrClosed
	}

	// Create reader if not exists
	if uf.reader == nil {
		reader, err := uf.createUsenetReader(uf.ctx, uf.position, uf.size-1)
		if err != nil {
			return 0, fmt.Errorf("failed to create usenet reader: %w", err)
		}
		uf.reader = reader
	}

	n, err = uf.reader.Read(p)
	uf.position += int64(n)
	return n, err
}

func (uf *UsenetFile) Close() error {
	if uf.closed {
		return nil
	}

	uf.closed = true

	if uf.reader != nil {
		return uf.reader.Close()
	}

	return nil
}

// Seek implements io.Seeker interface for efficient RAR part access
func (uf *UsenetFile) Seek(offset int64, whence int) (int64, error) {
	if uf.closed {
		return 0, fs.ErrClosed
	}

	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = uf.position + offset
	case io.SeekEnd:
		abs = uf.size + offset
	default:
		return 0, fmt.Errorf("invalid whence value: %d", whence)
	}

	if abs < 0 {
		return 0, fmt.Errorf("negative seek position: %d", abs)
	}

	if abs > uf.size {
		return 0, fmt.Errorf("seek position beyond file size: %d > %d", abs, uf.size)
	}

	// If seeking to a different position, close current reader so it gets recreated
	if abs != uf.position && uf.reader != nil {
		uf.reader.Close()
		uf.reader = nil
	}

	uf.position = abs
	return abs, nil
}

// createUsenetReader creates a Usenet reader for the specified range
func (uf *UsenetFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	// Filter segments for this specific file
	fileSegments := uf.getFileSegments()
	loader := dbSegmentLoader{segs: fileSegments}

	// If we have a stored segment size, use it to compute ranges
	hasFixedSize := uf.nzbFile.SegmentSize > 0
	segSize := uf.nzbFile.SegmentSize

	rg := usenet.GetSegmentsInRange(start, end, loader, hasFixedSize, segSize)
	return usenet.NewUsenetReader(ctx, uf.cp, rg, uf.maxWorkers)
}

// getFileSegments returns segments specific to this RAR file
func (uf *UsenetFile) getFileSegments() database.NzbSegments {
	// This is a simplified approach - in practice, you'd need to map
	// segments to specific files within the NZB
	// For now, return the file's segments directly
	return uf.file.Segments
}

// dbSegmentLoader implements the segment loader interface for database segments
type dbSegmentLoader struct {
	segs database.NzbSegments
}

func (dl dbSegmentLoader) GetSegmentCount() int {
	return len(dl.segs)
}

func (dl dbSegmentLoader) GetSegment(index int) (segment nzbparser.NzbSegment, groups []string, ok bool) {
	if index < 0 || index >= len(dl.segs) {
		return nzbparser.NzbSegment{}, nil, false
	}
	seg := dl.segs[index]
	nzbSeg := nzbparser.NzbSegment{
		Number: seg.Number,
		Bytes:  int(seg.Bytes),
		ID:     seg.MessageID,
	}
	return nzbSeg, seg.Groups, true
}

// UsenetFileInfo methods implementing fs.FileInfo interface

func (ufi *UsenetFileInfo) Name() string       { return ufi.name }
func (ufi *UsenetFileInfo) Size() int64        { return ufi.size }
func (ufi *UsenetFileInfo) Mode() fs.FileMode  { return 0644 }
func (ufi *UsenetFileInfo) ModTime() time.Time { return time.Now() }
func (ufi *UsenetFileInfo) IsDir() bool        { return false }
func (ufi *UsenetFileInfo) Sys() interface{}   { return nil }
