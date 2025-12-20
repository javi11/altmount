package filesystem

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/spf13/afero"

	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/progress"
	"github.com/javi11/altmount/internal/slogutil"
	"github.com/javi11/altmount/internal/usenet"
)

// Compile-time interface checks
var (
	_ fs.File     = (*UsenetFile)(nil)       // UsenetFile implements fs.File
	_ io.Seeker   = (*UsenetFile)(nil)       // UsenetFile implements io.Seeker
	_ io.ReaderAt = (*UsenetFile)(nil)       // UsenetFile implements io.ReaderAt
	_ fs.FS       = (*UsenetFileSystem)(nil) // UsenetFileSystem implements fs.FS
)

// UsenetFileSystem implements fs.FS for reading RAR archives from Usenet
// This allows rardecode.OpenReader to access multi-part RAR files without downloading them entirely
type UsenetFileSystem struct {
	ctx             context.Context
	poolManager     pool.Manager
	files           map[string]parser.ParsedFile
	maxWorkers      int
	maxCacheSizeMB  int
	progressTracker *progress.Tracker
	filesCompleted  int32 // atomic counter
	totalFiles      int
}

// UsenetFileInfo implements fs.FileInfo for RAR part files
type UsenetFileInfo struct {
	name string
	size int64
}

// NewUsenetFileSystem creates a new filesystem for accessing RAR parts from Usenet
func NewUsenetFileSystem(ctx context.Context, poolManager pool.Manager, files []parser.ParsedFile, maxWorkers int, maxCacheSizeMB int, progressTracker *progress.Tracker) *UsenetFileSystem {
	filesMap := make(map[string]parser.ParsedFile)
	for _, file := range files {
		filesMap[file.Filename] = file
	}

	return &UsenetFileSystem{
		ctx:             ctx,
		poolManager:     poolManager,
		files:           filesMap,
		maxWorkers:      maxWorkers,
		maxCacheSizeMB:  maxCacheSizeMB,
		progressTracker: progressTracker,
		filesCompleted:  0,
		totalFiles:      len(files),
	}
}

// Open opens a file in the Usenet filesystem
func (ufs *UsenetFileSystem) Open(name string) (fs.File, error) {
	name = path.Clean(name)

	ctx := slogutil.With(ufs.ctx, "file_name", name)

	// Find the corresponding RAR file
	file, ok := ufs.files[name]
	if !ok {
		return nil, &fs.PathError{
			Op:   "open",
			Path: name,
			Err:  fs.ErrNotExist,
		}
	}

	return &UsenetFile{
		name:           name,
		file:           &file,
		poolManager:    ufs.poolManager,
		ctx:            ctx,
		maxWorkers:     ufs.maxWorkers,
		maxCacheSizeMB: ufs.maxCacheSizeMB,
		size:           file.Size,
		position:       0,
		closed:         false,
		ufs:            ufs,
	}, nil
}

// Stat returns file information for a file in the Usenet filesystem
// This implements the rarlist.FileSystem interface
func (ufs *UsenetFileSystem) Stat(path string) (os.FileInfo, error) {
	path = filepath.Clean(path)

	// Find the corresponding RAR file
	file, ok := ufs.files[path]
	if !ok {
		return nil, &fs.PathError{
			Op:   "stat",
			Path: path,
			Err:  fs.ErrNotExist,
		}
	}

	return &UsenetFileInfo{
		name: filepath.Base(file.Filename),
		size: file.Size,
	}, nil
}

// UsenetFile implements fs.File and io.Seeker for reading individual RAR parts from Usenet
// The Seeker interface allows rardecode.OpenReader to efficiently seek within RAR parts
type UsenetFile struct {
	name           string
	file           *parser.ParsedFile
	poolManager    pool.Manager
	ctx            context.Context
	maxWorkers     int
	maxCacheSizeMB int
	size           int64
	reader         io.ReadCloser
	position       int64
	closed         bool
	ufs            *UsenetFileSystem
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

	var closeErr error
	if uf.reader != nil {
		closeErr = uf.reader.Close()
	}

	// Report progress on file close
	if uf.ufs != nil && uf.ufs.progressTracker != nil {
		completed := atomic.AddInt32(&uf.ufs.filesCompleted, 1)
		uf.ufs.progressTracker.Update(int(completed), uf.ufs.totalFiles)
	}

	return closeErr
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

// ReadAt implements io.ReaderAt interface for 7zip access
// ReadAt reads len(p) bytes into p starting at offset off in the file
// It returns the number of bytes read and any error encountered
func (uf *UsenetFile) ReadAt(p []byte, off int64) (n int, err error) {
	if uf.closed {
		return 0, fs.ErrClosed
	}

	if off < 0 {
		return 0, fmt.Errorf("negative offset: %d", off)
	}

	if off >= uf.size {
		return 0, io.EOF
	}

	// Calculate the end position for this read
	end := off + int64(len(p)) - 1
	if end >= uf.size {
		end = uf.size - 1
	}

	// Create a timeout context to prevent indefinite blocking
	ctx, cancel := context.WithTimeout(uf.ctx, 5*time.Minute)
	defer cancel()

	// Create a new reader for this specific range
	reader, err := uf.createUsenetReader(ctx, off, end)
	if err != nil {
		return 0, fmt.Errorf("failed to create usenet reader: %w", err)
	}
	defer reader.Close()

	// Read from the reader with timeout handling
	type readResult struct {
		n   int
		err error
	}
	done := make(chan readResult, 1)

	go func() {
		n, err := io.ReadFull(reader, p)
		done <- readResult{n, err}
	}()

	select {
	case result := <-done:
		return result.n, result.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// createUsenetReader creates a Usenet reader for the specified range
func (uf *UsenetFile) createUsenetReader(ctx context.Context, start, end int64) (io.ReadCloser, error) {
	// Filter segments for this specific file
	loader := dbSegmentLoader{segs: uf.file.Segments}

	if loader.GetSegmentCount() == 0 {
		slog.ErrorContext(ctx, "[importer.UsenetFile] No segments to download", "start", start, "end", end)

		return nil, fmt.Errorf("[importer.UsenetFile] no segments to download")
	}

	rg := usenet.GetSegmentsInRange(start, end, loader)
	return usenet.NewUsenetReader(ctx, uf.poolManager.GetPool, rg, uf.maxWorkers, uf.maxCacheSizeMB)
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
		End:   seg.EndOffset,
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

// AferoAdapter wraps UsenetFileSystem to implement afero.Fs interface
// This allows sevenzip.OpenReader to use UsenetFileSystem as a custom filesystem
type AferoAdapter struct {
	ufs *UsenetFileSystem
}

// NewAferoAdapter creates a new Afero filesystem adapter for UsenetFileSystem
func NewAferoAdapter(ufs *UsenetFileSystem) afero.Fs {
	return &AferoAdapter{ufs: ufs}
}

// Compile-time interface check
var _ afero.Fs = (*AferoAdapter)(nil)

// Read-only operations (delegate to UsenetFileSystem)

func (a *AferoAdapter) Open(name string) (afero.File, error) {
	file, err := a.ufs.Open(name)
	if err != nil {
		return nil, err
	}
	// Wrap fs.File to afero.File
	return &aferoFileAdapter{file: file}, nil
}

func (a *AferoAdapter) Stat(name string) (os.FileInfo, error) {
	return a.ufs.Stat(name)
}

func (a *AferoAdapter) Name() string {
	return "UsenetFileSystem"
}

// Write operations (not supported - return errors)

var ErrReadOnlyFilesystem = errors.New("filesystem is read-only")

func (a *AferoAdapter) Create(name string) (afero.File, error) {
	return nil, ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Mkdir(name string, perm os.FileMode) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) MkdirAll(path string, perm os.FileMode) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Remove(name string) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) RemoveAll(path string) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Rename(oldname, newname string) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Chmod(name string, mode os.FileMode) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Chown(name string, uid, gid int) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) Chtimes(name string, atime time.Time, mtime time.Time) error {
	return ErrReadOnlyFilesystem
}

func (a *AferoAdapter) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	// Only support read-only operations
	if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC|os.O_APPEND) != 0 {
		return nil, ErrReadOnlyFilesystem
	}
	return a.Open(name)
}

// aferoFileAdapter wraps fs.File to implement afero.File interface
type aferoFileAdapter struct {
	file fs.File
}

// Compile-time interface check
var _ afero.File = (*aferoFileAdapter)(nil)

func (a *aferoFileAdapter) Close() error {
	return a.file.Close()
}

func (a *aferoFileAdapter) Read(p []byte) (n int, err error) {
	return a.file.Read(p)
}

func (a *aferoFileAdapter) ReadAt(p []byte, off int64) (n int, err error) {
	if ra, ok := a.file.(io.ReaderAt); ok {
		return ra.ReadAt(p, off)
	}
	return 0, errors.New("ReadAt not supported")
}

func (a *aferoFileAdapter) Seek(offset int64, whence int) (int64, error) {
	if seeker, ok := a.file.(io.Seeker); ok {
		return seeker.Seek(offset, whence)
	}
	return 0, errors.New("Seek not supported")
}

func (a *aferoFileAdapter) Write(p []byte) (n int, err error) {
	return 0, ErrReadOnlyFilesystem
}

func (a *aferoFileAdapter) WriteAt(p []byte, off int64) (n int, err error) {
	return 0, ErrReadOnlyFilesystem
}

func (a *aferoFileAdapter) Name() string {
	if namer, ok := a.file.(interface{ Name() string }); ok {
		return namer.Name()
	}
	return ""
}

func (a *aferoFileAdapter) Readdir(count int) ([]os.FileInfo, error) {
	return nil, errors.New("Readdir not supported")
}

func (a *aferoFileAdapter) Readdirnames(n int) ([]string, error) {
	return nil, errors.New("Readdirnames not supported")
}

func (a *aferoFileAdapter) Stat() (os.FileInfo, error) {
	return a.file.Stat()
}

func (a *aferoFileAdapter) Sync() error {
	return nil // No-op for read-only filesystem
}

func (a *aferoFileAdapter) Truncate(size int64) error {
	return ErrReadOnlyFilesystem
}

func (a *aferoFileAdapter) WriteString(s string) (ret int, err error) {
	return 0, ErrReadOnlyFilesystem
}
