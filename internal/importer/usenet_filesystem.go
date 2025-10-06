package importer

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/nntppool"
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
	ctx            context.Context
	cp             nntppool.UsenetConnectionPool
	files          []ParsedFile
	maxWorkers     int
	maxCacheSizeMB int
	log            *slog.Logger
}

// UsenetFile implements fs.File and io.Seeker for reading individual RAR parts from Usenet
// The Seeker interface allows rardecode.OpenReader to efficiently seek within RAR parts
type UsenetFile struct {
	name           string
	file           *ParsedFile
	cp             nntppool.UsenetConnectionPool
	ctx            context.Context
	maxWorkers     int
	maxCacheSizeMB int
	size           int64
	reader         io.ReadCloser
	position       int64
	closed         bool
}

// UsenetFileInfo implements fs.FileInfo for RAR part files
type UsenetFileInfo struct {
	name string
	size int64
}

// NewUsenetFileSystem creates a new filesystem for accessing RAR parts from Usenet
func NewUsenetFileSystem(ctx context.Context, cp nntppool.UsenetConnectionPool, files []ParsedFile, maxWorkers int, maxCacheSizeMB int, log *slog.Logger) *UsenetFileSystem {
	return &UsenetFileSystem{
		ctx:            ctx,
		cp:             cp,
		files:          files,
		maxWorkers:     maxWorkers,
		maxCacheSizeMB: maxCacheSizeMB,
		log:            log,
	}
}

// normalizeRarFilename strips trailing ] bracket from RAR filenames for matching
// This handles cases where some parts have brackets (e.g., "file.r20]") and others don't
func normalizeRarFilename(name string) string {
	return strings.TrimSuffix(name, "]")
}

// Open opens a file in the Usenet filesystem
func (ufs *UsenetFileSystem) Open(name string) (fs.File, error) {
	name = path.Clean(name)
	
	// Normalize requested name for comparison (strip trailing bracket if present)
	normalizedName := normalizeRarFilename(name)
	normalizedBaseName := normalizeRarFilename(path.Base(name))

	ufs.log.Debug("rarlist requesting file",
		"requested_name", name,
		"normalized_name", normalizedName,
		"available_files_count", len(ufs.files))

	// Find the corresponding RAR file using normalized matching
	// This handles cases where rarlist requests "file.r20" but actual filename is "file.r20]"
	for _, file := range ufs.files {
		normalizedFilename := normalizeRarFilename(file.Filename)
		normalizedFileBase := normalizeRarFilename(path.Base(file.Filename))
		
		if normalizedFilename == normalizedName || 
		   normalizedFileBase == normalizedBaseName ||
		   file.Filename == name || 
		   path.Base(file.Filename) == name {
			ufs.log.Debug("File matched",
				"requested", name,
				"matched_file", file.Filename,
				"match_type", func() string {
					if normalizedFilename == normalizedName {
						return "normalized_full"
					} else if normalizedFileBase == normalizedBaseName {
						return "normalized_base"
					}
					return "exact"
				}())
			return &UsenetFile{
				name:           name,
				file:           &file,
				cp:             ufs.cp,
				ctx:            ufs.ctx,
				maxWorkers:     ufs.maxWorkers,
				maxCacheSizeMB: ufs.maxCacheSizeMB,
				size:           file.Size,
				position:       0,
				closed:         false,
			}, nil
		}
	}

	ufs.log.Warn("rarlist requested file not found",
		"requested", name,
		"normalized", normalizedName,
		"available_files", func() []string {
			names := make([]string, len(ufs.files))
			for i, f := range ufs.files {
				names[i] = f.Filename
			}
			return names
		}())

	return nil, &fs.PathError{
		Op:   "open",
		Path: name,
		Err:  fs.ErrNotExist,
	}
}

// Stat returns file information for a file in the Usenet filesystem
// This implements the rarlist.FileSystem interface
func (ufs *UsenetFileSystem) Stat(path string) (os.FileInfo, error) {
	path = filepath.Clean(path)
	
	// Normalize path for comparison (strip trailing bracket if present)
	normalizedPath := normalizeRarFilename(path)
	normalizedBasePath := normalizeRarFilename(filepath.Base(path))

	// Find the corresponding RAR file using normalized matching
	for _, file := range ufs.files {
		normalizedFilename := normalizeRarFilename(file.Filename)
		normalizedFileBase := normalizeRarFilename(filepath.Base(file.Filename))
		
		if normalizedFilename == normalizedPath || 
		   normalizedFileBase == normalizedBasePath ||
		   file.Filename == path || 
		   filepath.Base(file.Filename) == path {
			return &UsenetFileInfo{
				name: filepath.Base(file.Filename),
				size: file.Size,
			}, nil
		}
	}

	return nil, &fs.PathError{
		Op:   "stat",
		Path: path,
		Err:  fs.ErrNotExist,
	}
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
	loader := dbSegmentLoader{segs: uf.file.Segments}

	rg := usenet.GetSegmentsInRange(start, end, loader)
	return usenet.NewUsenetReader(ctx, uf.cp, rg, uf.maxWorkers, uf.maxCacheSizeMB)
}

// dbSegmentLoader implements the segment loader interface for database segments
type dbSegmentLoader struct {
	segs []*metapb.SegmentData
}

func (dl dbSegmentLoader) GetSegmentCount() int {
	return len(dl.segs)
}

func (dl dbSegmentLoader) GetSegment(index int) (segment usenet.Segment, groups []string, ok bool) {
	if index < 0 || index >= len(dl.segs) {
		return usenet.Segment{}, nil, false
	}
	seg := dl.segs[index]

	return usenet.Segment{
		Id:    seg.Id,
		Start: seg.StartOffset,
		Size:  seg.SegmentSize,
	}, nil, true
}

// UsenetFileInfo methods implementing fs.FileInfo interface

func (ufi *UsenetFileInfo) Name() string       { return ufi.name }
func (ufi *UsenetFileInfo) Size() int64        { return ufi.size }
func (ufi *UsenetFileInfo) Mode() fs.FileMode  { return 0644 }
func (ufi *UsenetFileInfo) ModTime() time.Time { return time.Now() }
func (ufi *UsenetFileInfo) IsDir() bool        { return false }
func (ufi *UsenetFileInfo) Sys() interface{}   { return nil }
