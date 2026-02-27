package webdav

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"golang.org/x/net/webdav"
)

type fileSystem struct {
	nzbFs     *nzbfilesystem.NzbFilesystem
	importDir string
}

func nzbToWebdavFS(nzbFs *nzbfilesystem.NzbFilesystem, importDir string) webdav.FileSystem {
	return &fileSystem{
		nzbFs:     nzbFs,
		importDir: importDir,
	}
}

func (fs *fileSystem) resolvePath(name string) (string, bool) {
	if fs.importDir != "" && (name == "/library" || strings.HasPrefix(name, "/library/")) {
		rel := strings.TrimPrefix(name, "/library")
		if rel == "" {
			rel = "/"
		}
		return filepath.Join(fs.importDir, rel), true
	}
	return name, false
}

func (fs *fileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	if path, isLibrary := fs.resolvePath(name); isLibrary {
		return os.Mkdir(path, perm)
	}
	return fs.nzbFs.Mkdir(ctx, name, perm)
}

func (fs *fileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	if name == "/" {
		f, err := fs.nzbFs.OpenFile(ctx, name, flag, perm)
		if err != nil {
			return nil, err
		}
		return &rootFile{File: f, hasLibrary: fs.importDir != ""}, nil
	}

	if path, isLibrary := fs.resolvePath(name); isLibrary {
		f, err := os.OpenFile(path, flag, perm)
		if err != nil {
			return nil, err
		}
		return f, nil
	}
	return fs.nzbFs.OpenFile(ctx, name, flag, perm)
}

type rootFile struct {
	webdav.File
	hasLibrary bool
	added      bool
}

func (r *rootFile) Readdir(count int) ([]os.FileInfo, error) {
	infos, err := r.File.Readdir(count)
	if err != nil && (err != io.EOF || r.added || !r.hasLibrary) {
		return nil, err
	}

	if r.hasLibrary && !r.added && (count <= 0 || len(infos) < count) {
		r.added = true
		libStat, err := os.Stat(".") // Placeholder or actual importDir stat
		if err == nil {
			// Create a virtual FileInfo for 'library'
			infos = append(infos, &virtualDirInfo{name: "library", modTime: libStat.ModTime()})
		}
		// If we reached EOF on underlying FS but added library, we might need to suppress EOF if count > 0
		return infos, nil
	}

	return infos, err
}

type virtualDirInfo struct {
	name    string
	modTime time.Time
}

func (v *virtualDirInfo) Name() string       { return v.name }
func (v *virtualDirInfo) Size() int64        { return 0 }
func (v *virtualDirInfo) Mode() os.FileMode  { return os.ModeDir | 0755 }
func (v *virtualDirInfo) ModTime() time.Time { return v.modTime }
func (v *virtualDirInfo) IsDir() bool        { return true }
func (v *virtualDirInfo) Sys() interface{}   { return nil }

func (fs *fileSystem) RemoveAll(ctx context.Context, name string) error {
	if path, isLibrary := fs.resolvePath(name); isLibrary {
		return os.RemoveAll(path)
	}
	return fs.nzbFs.RemoveAll(ctx, name)
}

func (fs *fileSystem) Rename(ctx context.Context, oldName, newName string) error {
	oldPath, oldIsLib := fs.resolvePath(oldName)
	newPath, newIsLib := fs.resolvePath(newName)

	if oldIsLib || newIsLib {
		if oldIsLib && newIsLib {
			return os.Rename(oldPath, newPath)
		}
		return os.ErrPermission
	}

	// Add logging to understand when MOVE operations trigger renames
	slog.InfoContext(ctx, "WebDAV filesystem Rename called",
		"oldName", oldName,
		"newName", newName)
	return fs.nzbFs.Rename(ctx, oldName, newName)
}

func (fs *fileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	if name == "/library" && fs.importDir != "" {
		return os.Stat(fs.importDir)
	}
	if path, isLibrary := fs.resolvePath(name); isLibrary {
		return os.Stat(path)
	}
	return fs.nzbFs.Stat(ctx, name)
}
