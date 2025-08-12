package nzbfilesystem

import (
	"errors"
	"os"
)

// Readdir lists directory contents
func (vf *VirtualFile) Readdir(n int) ([]os.FileInfo, error) {
	if !vf.virtualFile.IsDirectory {
		return nil, ErrNotDirectory
	}

	// For root directory ("/"), list items with parent_id = NULL
	// For other directories, list items with parent_id = directory ID
	var parentID *int64
	if vf.virtualFile.VirtualPath == RootPath {
		parentID = nil // Root level items have parent_id = NULL
	} else {
		parentID = &vf.virtualFile.ID
	}

	children, err := vf.db.Repository.ListVirtualFilesByParentID(parentID)
	if err != nil {
		return nil, errors.Join(err, ErrFailedListDirectory)
	}

	var infos []os.FileInfo
	for _, child := range children {
		info := &VirtualFileInfo{
			name:    child.Filename,
			size:    child.Size,
			modTime: child.CreatedAt,
			isDir:   child.IsDirectory,
		}
		infos = append(infos, info)

		// If n > 0, limit the results
		if n > 0 && len(infos) >= n {
			break
		}
	}

	return infos, nil
}

// Readdirnames returns directory entry names
func (vf *VirtualFile) Readdirnames(n int) ([]string, error) {
	infos, err := vf.Readdir(n)
	if err != nil {
		return nil, err
	}

	names := make([]string, len(infos))
	for i, info := range infos {
		names[i] = info.Name()
	}

	return names, nil
}
