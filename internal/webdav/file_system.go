package webdav

import (
	"context"
	"log/slog"
	"os"

	"github.com/javi11/altmount/internal/nzbfilesystem"
	"github.com/javi11/altmount/internal/nzbfilesystem/segcache"
	"github.com/javi11/altmount/internal/utils"
	"github.com/spf13/afero"
	"golang.org/x/net/webdav"
)

type fileSystem struct {
	nzbFs       *nzbfilesystem.NzbFilesystem
	segcacheMgr *segcache.Manager // nil when segment cache is disabled
}

func nzbToWebdavFS(nzbFs *nzbfilesystem.NzbFilesystem, segcacheMgr *segcache.Manager) webdav.FileSystem {
	return &fileSystem{
		nzbFs:       nzbFs,
		segcacheMgr: segcacheMgr,
	}
}

func (fs *fileSystem) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	return fs.nzbFs.Mkdir(ctx, name, perm)
}

func (fs *fileSystem) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	// Segment cache mode: return a SegmentCachedFile that implements webdav.File.
	// The webdav.Handler calls Seek() for range requests then Read() sequentially —
	// both of which SegmentCachedFile implements over its ReadAt core.
	if fs.segcacheMgr != nil {
		entries, fileSize, err := fs.nzbFs.GetSegmentEntries(ctx, name)
		if err == nil && len(entries) > 0 {
			opener := &webdavSuppressedOpener{nzbFs: fs.nzbFs}
			segFile, openErr := fs.segcacheMgr.Open(name, entries, fileSize, opener)
			if openErr == nil {
				return segFile, nil
			}
			slog.WarnContext(ctx, "segcache: WebDAV Open failed, falling back",
				"path", name, "error", openErr)
		}
	}

	// Fallback: direct MetadataVirtualFile access.
	return fs.nzbFs.OpenFile(ctx, name, flag, perm)
}

func (fs *fileSystem) RemoveAll(ctx context.Context, name string) error {
	return fs.nzbFs.RemoveAll(ctx, name)
}

func (fs *fileSystem) Rename(ctx context.Context, oldName, newName string) error {
	// Add logging to understand when MOVE operations trigger renames
	slog.InfoContext(ctx, "WebDAV filesystem Rename called",
		"oldName", oldName,
		"newName", newName)
	return fs.nzbFs.Rename(ctx, oldName, newName)
}

func (fs *fileSystem) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	return fs.nzbFs.Stat(ctx, name)
}

// webdavSuppressedOpener wraps NzbFilesystem.Open with SuppressStreamTrackingKey
// so background segment fetches inside the segment cache don't create duplicate streams.
type webdavSuppressedOpener struct {
	nzbFs *nzbfilesystem.NzbFilesystem
}

func (o *webdavSuppressedOpener) Open(ctx context.Context, name string) (afero.File, error) {
	ctx = context.WithValue(ctx, utils.SuppressStreamTrackingKey, true)
	return o.nzbFs.Open(ctx, name)
}
