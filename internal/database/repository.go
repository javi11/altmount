package database

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// Repository provides database operations for NZB and file management
type Repository struct {
	db interface {
		Exec(query string, args ...interface{}) (sql.Result, error)
		Query(query string, args ...interface{}) (*sql.Rows, error)
		QueryRow(query string, args ...interface{}) *sql.Row
	}
}

// NewRepository creates a new repository instance
func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// NZB File operations

// CreateNzbFile inserts a new NZB file record
func (r *Repository) CreateNzbFile(nzbFile *NzbFile) error {
	query := `
		INSERT INTO nzb_files (path, filename, size, nzb_type, segments_count, segments_data, segment_size, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query, nzbFile.Path, nzbFile.Filename, nzbFile.Size,
		nzbFile.NzbType, nzbFile.SegmentsCount, nzbFile.SegmentsData, nzbFile.SegmentSize, time.Now())
	if err != nil {
		return fmt.Errorf("failed to create nzb file: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get nzb file id: %w", err)
	}

	nzbFile.ID = id
	return nil
}

// GetNzbFileByPath retrieves an NZB file by its path
func (r *Repository) GetNzbFileByPath(path string) (*NzbFile, error) {
	query := `
		SELECT id, path, filename, size, created_at, updated_at, nzb_type, segments_count, segments_data, segment_size
		FROM nzb_files WHERE path = ?
	`

	var nzbFile NzbFile
	err := r.db.QueryRow(query, path).Scan(
		&nzbFile.ID, &nzbFile.Path, &nzbFile.Filename, &nzbFile.Size,
		&nzbFile.CreatedAt, &nzbFile.UpdatedAt, &nzbFile.NzbType,
		&nzbFile.SegmentsCount, &nzbFile.SegmentsData, &nzbFile.SegmentSize,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get nzb file: %w", err)
	}

	return &nzbFile, nil
}

// DeleteNzbFile removes an NZB file and all associated records
func (r *Repository) DeleteNzbFile(id int64) error {
	query := `DELETE FROM nzb_files WHERE id = ?`

	_, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete nzb file: %w", err)
	}

	return nil
}

// Virtual File operations

// CreateVirtualFile inserts a new virtual file record
func (r *Repository) CreateVirtualFile(vf *VirtualFile) error {
	query := `
		INSERT INTO virtual_files (nzb_file_id, parent_id, virtual_path, filename, size, is_directory, encryption)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query, vf.NzbFileID, vf.ParentID, vf.VirtualPath, vf.Filename,
		vf.Size, vf.IsDirectory, vf.Encryption)
	if err != nil {
		return fmt.Errorf("failed to create virtual file: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get virtual file id: %w", err)
	}

	vf.ID = id
	return nil
}

// GetVirtualFileByPath retrieves a virtual file by its path
func (r *Repository) GetVirtualFileByPath(path string) (*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
		FROM virtual_files WHERE virtual_path = ?
	`

	var vf VirtualFile
	err := r.db.QueryRow(query, path).Scan(
		&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get virtual file: %w", err)
	}

	return &vf, nil
}

// ListVirtualFilesByParentID retrieves virtual files under a parent directory by parent ID
func (r *Repository) ListVirtualFilesByParentID(parentID *int64) ([]*VirtualFile, error) {
	var query string
	var args []interface{}
	
	if parentID == nil {
		// List root level files (parent_id IS NULL)
		query = `
			SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
			FROM virtual_files WHERE parent_id IS NULL ORDER BY is_directory DESC, filename ASC
		`
	} else {
		// List files with specific parent ID
		query = `
			SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
			FROM virtual_files WHERE parent_id = ? ORDER BY is_directory DESC, filename ASC
		`
		args = append(args, *parentID)
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list virtual files: %w", err)
	}
	defer rows.Close()

	var files []*VirtualFile
	for rows.Next() {
		var vf VirtualFile
		err := rows.Scan(
			&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath, &vf.Filename,
			&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan virtual file: %w", err)
		}
		files = append(files, &vf)
	}

	return files, rows.Err()
}

// ListVirtualFilesByParentPath retrieves virtual files under a parent directory (legacy method)
func (r *Repository) ListVirtualFilesByParentPath(parentPath string) ([]*VirtualFile, error) {
	// Find parent directory by path first
	parent, err := r.GetVirtualFileByPath(parentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get parent directory: %w", err)
	}

	if parent == nil {
		// Parent path not found, check if it's root
		if parentPath == "/" {
			return r.ListVirtualFilesByParentID(nil)
		}
		return nil, fmt.Errorf("parent directory not found: %s", parentPath)
	}

	return r.ListVirtualFilesByParentID(&parent.ID)
}

// GetVirtualFileWithNzb retrieves a virtual file along with its NZB file data (if any)
func (r *Repository) GetVirtualFileWithNzb(path string) (*VirtualFile, *NzbFile, error) {
	query := `
		SELECT vf.id, vf.nzb_file_id, vf.parent_id, vf.virtual_path, vf.filename, vf.size, vf.created_at, vf.is_directory, vf.encryption,
		       nf.id, nf.path, nf.filename, nf.size, nf.created_at, nf.updated_at, nf.nzb_type, nf.segments_count, nf.segments_data, nf.segment_size
		FROM virtual_files vf
		LEFT JOIN nzb_files nf ON vf.nzb_file_id = nf.id
		WHERE vf.virtual_path = ?
	`

	var vf VirtualFile
	var nf NzbFile
	// Use nullable types for NZB fields since they might be NULL due to LEFT JOIN
	var nzbID sql.NullInt64
	var nzbPath sql.NullString
	var nzbFilename sql.NullString
	var nzbSize sql.NullInt64
	var nzbCreatedAt sql.NullTime
	var nzbUpdatedAt sql.NullTime
	var nzbType sql.NullString
	var nzbSegmentsCount sql.NullInt64
	var nzbSegmentsData sql.NullString
	var nzbSegmentSize sql.NullInt64

	err := r.db.QueryRow(query, path).Scan(
		&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
		&nzbID, &nzbPath, &nzbFilename, &nzbSize,
		&nzbCreatedAt, &nzbUpdatedAt, &nzbType,
		&nzbSegmentsCount, &nzbSegmentsData, &nzbSegmentSize,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to get virtual file with nzb: %w", err)
	}

	// If no NZB data (system directory like root), return virtual file only
	if !nzbID.Valid {
		return &vf, nil, nil
	}

	// Convert nullable types to NzbFile struct
	nf.ID = nzbID.Int64
	nf.Path = nzbPath.String
	nf.Filename = nzbFilename.String
	nf.Size = nzbSize.Int64
	nf.CreatedAt = nzbCreatedAt.Time
	nf.UpdatedAt = nzbUpdatedAt.Time
	nf.NzbType = NzbType(nzbType.String)
	nf.SegmentsCount = int(nzbSegmentsCount.Int64)
	nf.SegmentSize = nzbSegmentSize.Int64
	
	// Parse segments data JSON
	if nzbSegmentsData.Valid && nzbSegmentsData.String != "" {
		if err := nf.SegmentsData.Scan(nzbSegmentsData.String); err != nil {
			return nil, nil, fmt.Errorf("failed to parse segments data: %w", err)
		}
	}

	return &vf, &nf, nil
}

// RAR Content operations

// CreateRarContent inserts a new RAR content record
func (r *Repository) CreateRarContent(rc *RarContent) error {
	query := `
		INSERT INTO rar_contents (virtual_file_id, internal_path, filename, size, compressed_size, crc32)
		VALUES (?, ?, ?, ?, ?, ?)
	`

	result, err := r.db.Exec(query, rc.VirtualFileID, rc.InternalPath, rc.Filename,
		rc.Size, rc.CompressedSize, rc.CRC32)
	if err != nil {
		return fmt.Errorf("failed to create rar content: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get rar content id: %w", err)
	}

	rc.ID = id
	return nil
}

// GetRarContentsByVirtualFileID retrieves RAR contents for a virtual file
func (r *Repository) GetRarContentsByVirtualFileID(virtualFileID int64) ([]*RarContent, error) {
	query := `
		SELECT id, virtual_file_id, internal_path, filename, size, compressed_size, crc32, created_at
		FROM rar_contents WHERE virtual_file_id = ? ORDER BY internal_path ASC
	`

	rows, err := r.db.Query(query, virtualFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to list rar contents: %w", err)
	}
	defer rows.Close()

	var contents []*RarContent
	for rows.Next() {
		var rc RarContent
		err := rows.Scan(
			&rc.ID, &rc.VirtualFileID, &rc.InternalPath, &rc.Filename,
			&rc.Size, &rc.CompressedSize, &rc.CRC32, &rc.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan rar content: %w", err)
		}
		contents = append(contents, &rc)
	}

	return contents, rows.Err()
}

// File Metadata operations

// SetFileMetadata sets a metadata key-value pair for a virtual file
func (r *Repository) SetFileMetadata(virtualFileID int64, key, value string) error {
	query := `
		INSERT OR REPLACE INTO file_metadata (virtual_file_id, key, value, created_at)
		VALUES (?, ?, ?, ?)
	`

	_, err := r.db.Exec(query, virtualFileID, key, value, time.Now())
	if err != nil {
		return fmt.Errorf("failed to set file metadata: %w", err)
	}

	return nil
}

// GetFileMetadata retrieves metadata for a virtual file
func (r *Repository) GetFileMetadata(virtualFileID int64) (map[string]string, error) {
	query := `
		SELECT key, value FROM file_metadata WHERE virtual_file_id = ?
	`

	rows, err := r.db.Query(query, virtualFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to get file metadata: %w", err)
	}
	defer rows.Close()

	metadata := make(map[string]string)
	for rows.Next() {
		var key, value string
		err := rows.Scan(&key, &value)
		if err != nil {
			return nil, fmt.Errorf("failed to scan metadata: %w", err)
		}
		metadata[key] = value
	}

	return metadata, rows.Err()
}

// Transaction support

// WithTransaction executes a function within a database transaction
func (r *Repository) WithTransaction(fn func(*Repository) error) error {
	// Cast to *sql.DB to access Begin method
	sqlDB, ok := r.db.(*sql.DB)
	if !ok {
		return fmt.Errorf("repository not connected to sql.DB")
	}

	tx, err := sqlDB.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}

	txRepo := &Repository{db: tx}

	err = fn(txRepo)
	if err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("failed to rollback transaction (original error: %w): %w", err, rollbackErr)
		}
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	return nil
}

// Utility methods

// PathExists checks if a virtual path exists
func (r *Repository) PathExists(path string) (bool, error) {
	query := `SELECT 1 FROM virtual_files WHERE virtual_path = ? LIMIT 1`

	var exists int
	err := r.db.QueryRow(query, path).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("failed to check path existence: %w", err)
	}

	return true, nil
}

// GetDirectoryTree returns the complete directory tree structure
func (r *Repository) GetDirectoryTree() (map[int64][]*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
		FROM virtual_files ORDER BY parent_id, is_directory DESC, filename ASC
	`

	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory tree: %w", err)
	}
	defer rows.Close()

	tree := make(map[int64][]*VirtualFile)

	for rows.Next() {
		var vf VirtualFile
		err := rows.Scan(
			&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath, &vf.Filename,
			&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan virtual file: %w", err)
		}

		parentID := int64(0) // Use 0 for root level files (parent_id IS NULL)
		if vf.ParentID != nil {
			parentID = *vf.ParentID
		}

		tree[parentID] = append(tree[parentID], &vf)
	}

	return tree, rows.Err()
}

// GetVirtualFile retrieves a virtual file by its ID
func (r *Repository) GetVirtualFile(id int64) (*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
		FROM virtual_files WHERE id = ?
	`

	var vf VirtualFile
	err := r.db.QueryRow(query, id).Scan(
		&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get virtual file: %w", err)
	}

	return &vf, nil
}

// DeleteFileMetadata removes a specific metadata key for a virtual file
func (r *Repository) DeleteFileMetadata(virtualFileID int64, key string) error {
	query := `DELETE FROM file_metadata WHERE virtual_file_id = ? AND key = ?`

	_, err := r.db.Exec(query, virtualFileID, key)
	if err != nil {
		return fmt.Errorf("failed to delete file metadata: %w", err)
	}

	return nil
}

// Tree traversal and hierarchy methods

// GetParent retrieves the parent directory of a virtual file
func (r *Repository) GetParent(virtualFileID int64) (*VirtualFile, error) {
	query := `
		SELECT p.id, p.nzb_file_id, p.parent_id, p.virtual_path, p.filename, p.size, p.created_at, p.is_directory, p.encryption
		FROM virtual_files vf
		JOIN virtual_files p ON vf.parent_id = p.id
		WHERE vf.id = ?
	`

	var parent VirtualFile
	err := r.db.QueryRow(query, virtualFileID).Scan(
		&parent.ID, &parent.NzbFileID, &parent.ParentID, &parent.VirtualPath,
		&parent.Filename, &parent.Size, &parent.CreatedAt, &parent.IsDirectory, &parent.Encryption,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil // No parent (root level file)
		}
		return nil, fmt.Errorf("failed to get parent: %w", err)
	}

	return &parent, nil
}

// GetChildren retrieves all direct children of a directory
func (r *Repository) GetChildren(parentID int64) ([]*VirtualFile, error) {
	return r.ListVirtualFilesByParentID(&parentID)
}

// GetFullPath builds the complete path for a virtual file by traversing up the tree
func (r *Repository) GetFullPath(virtualFileID int64) (string, error) {
	// Use recursive CTE to build the full path
	query := `
		WITH RECURSIVE path_builder(id, filename, full_path, level) AS (
			SELECT id, filename, filename, 0 
			FROM virtual_files 
			WHERE id = ?
			UNION ALL
			SELECT vf.id, vf.filename, vf.filename || '/' || pb.full_path, pb.level + 1
			FROM virtual_files vf
			JOIN path_builder pb ON vf.id = pb.parent_id
		)
		SELECT full_path FROM path_builder 
		WHERE level = (SELECT MAX(level) FROM path_builder)
	`

	var fullPath string
	err := r.db.QueryRow(query, virtualFileID).Scan(&fullPath)
	if err != nil {
		if err == sql.ErrNoRows {
			// Fallback: get the file and use its virtual_path
			vf, err := r.GetVirtualFile(virtualFileID)
			if err != nil {
				return "", fmt.Errorf("failed to get virtual file for path: %w", err)
			}
			if vf == nil {
				return "", fmt.Errorf("virtual file not found")
			}
			return vf.VirtualPath, nil
		}
		return "", fmt.Errorf("failed to build full path: %w", err)
	}

	// Ensure path starts with /
	if !strings.HasPrefix(fullPath, "/") {
		fullPath = "/" + fullPath
	}

	return fullPath, nil
}

// GetDescendants retrieves all descendants (children, grandchildren, etc.) of a directory
func (r *Repository) GetDescendants(parentID int64) ([]*VirtualFile, error) {
	query := `
		WITH RECURSIVE descendants AS (
			SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption
			FROM virtual_files 
			WHERE id = ?
			UNION ALL
			SELECT vf.id, vf.nzb_file_id, vf.parent_id, vf.virtual_path, vf.filename, vf.size, vf.created_at, vf.is_directory, vf.encryption
			FROM virtual_files vf
			JOIN descendants d ON vf.parent_id = d.id
		)
		SELECT id, nzb_file_id, parent_id, virtual_path, filename, size, created_at, is_directory, encryption 
		FROM descendants 
		WHERE id != ? 
		ORDER BY virtual_path
	`

	rows, err := r.db.Query(query, parentID, parentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get descendants: %w", err)
	}
	defer rows.Close()

	var descendants []*VirtualFile
	for rows.Next() {
		var vf VirtualFile
		err := rows.Scan(
			&vf.ID, &vf.NzbFileID, &vf.ParentID, &vf.VirtualPath,
			&vf.Filename, &vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.Encryption,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan descendant: %w", err)
		}
		descendants = append(descendants, &vf)
	}

	return descendants, rows.Err()
}

// GetOrCreateDirectory ensures a directory exists, creating it if necessary
func (r *Repository) GetOrCreateDirectory(virtualPath string, parentID *int64) (*VirtualFile, error) {
	// Check if directory already exists
	existing, err := r.GetVirtualFileByPath(virtualPath)
	if err != nil {
		return nil, fmt.Errorf("failed to check existing directory: %w", err)
	}

	if existing != nil {
		if !existing.IsDirectory {
			return nil, fmt.Errorf("path exists but is not a directory: %s", virtualPath)
		}
		return existing, nil
	}

	// Create new directory
	filename := filepath.Base(virtualPath)
	dir := &VirtualFile{
		NzbFileID:   nil, // System directory
		ParentID:    parentID,
		VirtualPath: virtualPath,
		Filename:    filename,
		Size:        0,
		IsDirectory: true,
	}

	if err := r.CreateVirtualFile(dir); err != nil {
		return nil, fmt.Errorf("failed to create directory: %w", err)
	}

	return dir, nil
}

// MoveFile moves a file/directory to a new parent (updates parent_id)
func (r *Repository) MoveFile(virtualFileID int64, newParentID *int64, newVirtualPath string) error {
	query := `UPDATE virtual_files SET parent_id = ?, virtual_path = ? WHERE id = ?`

	_, err := r.db.Exec(query, newParentID, newVirtualPath, virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to move file: %w", err)
	}

	return nil
}

// DeleteVirtualFile removes a virtual file and all its descendants from the database
func (r *Repository) DeleteVirtualFile(virtualFileID int64) error {
	// Use CASCADE delete to automatically remove all descendants
	// The foreign key constraint will handle removing children when parent is deleted
	query := `DELETE FROM virtual_files WHERE id = ?`

	result, err := r.db.Exec(query, virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to delete virtual file: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("virtual file not found")
	}

	return nil
}

// UpdateVirtualFileFilename updates only the filename field of a virtual file
func (r *Repository) UpdateVirtualFileFilename(virtualFileID int64, newFilename string) error {
	query := `UPDATE virtual_files SET filename = ? WHERE id = ?`
	
	_, err := r.db.Exec(query, newFilename, virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to update filename: %w", err)
	}

	return nil
}

// UpdateVirtualFilePath updates only the virtual_path field of a virtual file
func (r *Repository) UpdateVirtualFilePath(virtualFileID int64, newPath string) error {
	query := `UPDATE virtual_files SET virtual_path = ? WHERE id = ?`
	
	_, err := r.db.Exec(query, newPath, virtualFileID)
	if err != nil {
		return fmt.Errorf("failed to update virtual path: %w", err)
	}

	return nil
}

// PAR2 File operations

// CreatePar2File inserts a new PAR2 file record
func (r *Repository) CreatePar2File(par2File *Par2File) error {
	query := `
		INSERT INTO par2_files (nzb_file_id, filename, size, segments_data)
		VALUES (?, ?, ?, ?)
	`

	result, err := r.db.Exec(query, par2File.NzbFileID, par2File.Filename,
		par2File.Size, par2File.SegmentsData)
	if err != nil {
		return fmt.Errorf("failed to create par2 file: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get par2 file id: %w", err)
	}

	par2File.ID = id
	return nil
}

// GetPar2FilesByNzbFileID retrieves all PAR2 files associated with an NZB file
func (r *Repository) GetPar2FilesByNzbFileID(nzbFileID int64) ([]*Par2File, error) {
	query := `
		SELECT id, nzb_file_id, filename, size, segments_data, created_at
		FROM par2_files WHERE nzb_file_id = ? ORDER BY filename ASC
	`

	rows, err := r.db.Query(query, nzbFileID)
	if err != nil {
		return nil, fmt.Errorf("failed to list par2 files: %w", err)
	}
	defer rows.Close()

	var par2Files []*Par2File
	for rows.Next() {
		var pf Par2File
		err := rows.Scan(
			&pf.ID, &pf.NzbFileID, &pf.Filename, &pf.Size,
			&pf.SegmentsData, &pf.CreatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan par2 file: %w", err)
		}
		par2Files = append(par2Files, &pf)
	}

	return par2Files, rows.Err()
}

// GetPar2FileByID retrieves a PAR2 file by its ID
func (r *Repository) GetPar2FileByID(id int64) (*Par2File, error) {
	query := `
		SELECT id, nzb_file_id, filename, size, segments_data, created_at
		FROM par2_files WHERE id = ?
	`

	var pf Par2File
	err := r.db.QueryRow(query, id).Scan(
		&pf.ID, &pf.NzbFileID, &pf.Filename, &pf.Size,
		&pf.SegmentsData, &pf.CreatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get par2 file: %w", err)
	}

	return &pf, nil
}

// DeletePar2FilesByNzbFileID removes all PAR2 files associated with an NZB file
func (r *Repository) DeletePar2FilesByNzbFileID(nzbFileID int64) error {
	query := `DELETE FROM par2_files WHERE nzb_file_id = ?`

	_, err := r.db.Exec(query, nzbFileID)
	if err != nil {
		return fmt.Errorf("failed to delete par2 files: %w", err)
	}

	return nil
}

// DeletePar2File removes a specific PAR2 file
func (r *Repository) DeletePar2File(id int64) error {
	query := `DELETE FROM par2_files WHERE id = ?`

	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to delete par2 file: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("par2 file not found")
	}

	return nil
}
