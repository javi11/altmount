package nzbfilesystem

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/utils"
	"github.com/javi11/nntppool"
	"github.com/spf13/afero"
)

// NzbRemoteFile implements the RemoteFile interface for NZB-backed virtual files
type NzbRemoteFile struct {
	db                 *database.DB
	cp                 nntppool.UsenetConnectionPool
	maxDownloadWorkers int
	rcloneCipher       encryption.Cipher // For rclone encryption/decryption
	headersCipher      encryption.Cipher // For headers encryption/decryption
	globalPassword     string            // Global password fallback
	globalSalt         string            // Global salt fallback
}

// OpenFile opens a virtual file backed by NZB data
func (nrf *NzbRemoteFile) OpenFile(ctx context.Context, name string, r utils.PathWithArgs) (bool, afero.File, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(name)

	// Check if this is a virtual file in our database
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedName)
	if err != nil {
		return false, nil, fmt.Errorf(ErrMsgFailedQueryVirtualFile, err)
	}

	if vf == nil {
		// File not found in database
		return false, nil, nil
	}

	// Get NZB data if this virtual file has associated NZB data
	var nzb *database.NzbFile
	if !vf.IsDirectory {
		nzb, err = nrf.db.Repository.GetNzbFileByID(vf.ID)
		if err != nil {
			return false, nil, fmt.Errorf("failed to get NZB file: %w", err)
		}
	}

	// Create a virtual file handle
	virtualFile := &VirtualFile{
		name:           name,
		virtualFile:    vf,
		nzbFile:        nzb, // Can be nil for system directories like root
		db:             nrf.db,
		args:           r,
		cp:             nrf.cp,
		ctx:            ctx,
		maxWorkers:     nrf.maxDownloadWorkers,
		rcloneCipher:   nrf.rcloneCipher,
		headersCipher:  nrf.headersCipher,
		globalPassword: nrf.globalPassword,
		globalSalt:     nrf.globalSalt,
	}

	// Note: Reader is now created lazily on first read operation to avoid memory leaks

	return true, virtualFile, nil
}

// RemoveFile removes a virtual file from the database
func (nrf *NzbRemoteFile) RemoveFile(ctx context.Context, fileName string) (bool, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Prevent removal of root directory
	if normalizedName == RootPath {
		return false, ErrCannotRemoveRoot
	}

	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedName)
	if err != nil {
		return false, fmt.Errorf(ErrMsgFailedQueryVirtualFile, err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// Use transaction to ensure atomicity
	err = nrf.db.Repository.WithTransaction(func(txRepo *database.Repository) error {
		// Delete the virtual file (CASCADE will handle all descendants automatically)
		// The foreign key constraint ON DELETE CASCADE will recursively remove all children
		if err := txRepo.DeleteVirtualFile(vf.ID); err != nil {
			return fmt.Errorf(ErrMsgFailedDeleteVirtualFile, err)
		}

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

// RenameFile renames a virtual file in the database
func (nrf *NzbRemoteFile) RenameFile(ctx context.Context, fileName, newFileName string) (bool, error) {
	// Normalize paths to handle trailing slashes consistently
	normalizedOldName := normalizePath(fileName)
	normalizedNewName := normalizePath(newFileName)

	// Prevent renaming the root directory
	if normalizedOldName == RootPath {
		return false, ErrCannotRenameRoot
	}

	// Prevent renaming to root directory
	if normalizedNewName == RootPath {
		return false, ErrCannotRenameToRoot
	}

	// Check if source file exists
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedOldName)
	if err != nil {
		return false, fmt.Errorf(ErrMsgFailedQueryVirtualFile, err)
	}

	if vf == nil {
		// File not found in database
		return false, nil
	}

	// Parse new path to get parent directory and filename
	newDir := filepath.Dir(normalizedNewName)
	newFilename := filepath.Base(normalizedNewName)

	// Ensure new directory path uses forward slashes
	newDir = strings.ReplaceAll(newDir, string(filepath.Separator), "/")
	if newDir == "." {
		newDir = RootPath
	}

	// Use transaction to ensure atomicity
	err = nrf.db.Repository.WithTransaction(func(txRepo *database.Repository) error {
		// Check if destination already exists
		existing, err := txRepo.GetVirtualFileByPath(normalizedNewName)
		if err != nil {
			return fmt.Errorf(ErrMsgFailedCheckDestination, err)
		}

		if existing != nil {
			return ErrDestinationExists
		}

		// Determine new parent ID
		var newParentID *int64
		if newDir == RootPath {
			// Moving to root - parent_id should be NULL
			newParentID = nil
		} else {
			// Find the parent directory
			parentDir, err := txRepo.GetVirtualFileByPath(newDir)
			if err != nil {
				return fmt.Errorf(ErrMsgFailedFindParent, err)
			}

			if parentDir == nil {
				return fmt.Errorf("parent directory does not exist: %s", newDir)
			}

			if !parentDir.IsDirectory {
				return fmt.Errorf("parent is not a directory: %s", newDir)
			}

			newParentID = &parentDir.ID
		}

		// Update the virtual file (move to new parent)
		if err := txRepo.MoveFile(vf.ID, newParentID); err != nil {
			return fmt.Errorf(ErrMsgFailedMoveFile, err)
		}

		// Update the filename if it changed
		if newFilename != vf.Name {
			if err := txRepo.UpdateVirtualFileName(vf.ID, newFilename); err != nil {
				return fmt.Errorf(ErrMsgFailedUpdateFilename, err)
			}
		}

		// If this is a directory, the descendants will automatically have
		// the correct parent relationships due to the hierarchical structure.
		// No need to update paths since they are calculated on demand.
		// The descendants are already correctly positioned under the moved directory.

		return nil
	})

	if err != nil {
		return false, err
	}

	return true, nil
}

// Stat returns file information for a virtual file
func (nrf *NzbRemoteFile) Stat(fileName string) (bool, fs.FileInfo, error) {
	// Normalize the path to handle trailing slashes consistently
	normalizedName := normalizePath(fileName)

	// Check if this is a virtual file
	vf, err := nrf.db.Repository.GetVirtualFileByPath(normalizedName)
	if err != nil {
		return false, nil, fmt.Errorf(ErrMsgFailedQueryVirtualFile, err)
	}

	if vf == nil {
		// File not found in database
		return false, nil, nil
	}

	// Create virtual file info
	virtualStat := &VirtualFileInfo{
		name:    vf.Name,
		size:    vf.Size,
		modTime: vf.CreatedAt,
		isDir:   vf.IsDirectory,
	}

	return true, virtualStat, nil
}
