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

// Queue operations

// AddToQueue adds an NZB file to the import queue
func (r *Repository) AddToQueue(item *ImportQueueItem) error {
	query := `
		INSERT INTO import_queue (nzb_path, watch_root, priority, status, retry_count, max_retries, batch_id, metadata, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(nzb_path) DO UPDATE SET
		priority = excluded.priority,
		batch_id = excluded.batch_id,
		metadata = excluded.metadata,
		updated_at = ?
	`

	result, err := r.db.Exec(query, 
		item.NzbPath, item.WatchRoot, item.Priority, item.Status,
		item.RetryCount, item.MaxRetries, item.BatchID, item.Metadata, time.Now(),
		time.Now())
	if err != nil {
		return fmt.Errorf("failed to add to queue: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return fmt.Errorf("failed to get queue item id: %w", err)
	}

	item.ID = id
	return nil
}

// GetNextQueueItems retrieves the next batch of items to process from the queue
func (r *Repository) GetNextQueueItems(limit int) ([]*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at, 
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
		FROM import_queue 
		WHERE status IN ('pending', 'retrying') AND retry_count < max_retries
		ORDER BY priority ASC, created_at ASC
		LIMIT ?
	`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("failed to get next queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// UpdateQueueItemStatus updates the status of a queue item
func (r *Repository) UpdateQueueItemStatus(id int64, status QueueStatus, errorMessage *string) error {
	now := time.Now()
	var query string
	var args []interface{}

	switch status {
	case QueueStatusProcessing:
		query = `UPDATE import_queue SET status = ?, started_at = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, now, now, id}
	case QueueStatusCompleted:
		query = `UPDATE import_queue SET status = ?, completed_at = ?, updated_at = ?, error_message = NULL WHERE id = ?`
		args = []interface{}{status, now, now, id}
	case QueueStatusFailed, QueueStatusRetrying:
		query = `UPDATE import_queue SET status = ?, retry_count = retry_count + 1, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	default:
		query = `UPDATE import_queue SET status = ?, error_message = ?, updated_at = ? WHERE id = ?`
		args = []interface{}{status, errorMessage, now, id}
	}

	_, err := r.db.Exec(query, args...)
	if err != nil {
		return fmt.Errorf("failed to update queue item status: %w", err)
	}

	return nil
}

// GetQueueItem retrieves a specific queue item by ID
func (r *Repository) GetQueueItem(id int64) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
		FROM import_queue WHERE id = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRow(query, id).Scan(
		&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item: %w", err)
	}

	return &item, nil
}

// GetQueueItemByPath retrieves a queue item by NZB path
func (r *Repository) GetQueueItemByPath(nzbPath string) (*ImportQueueItem, error) {
	query := `
		SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
		       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
		FROM import_queue WHERE nzb_path = ?
	`

	var item ImportQueueItem
	err := r.db.QueryRow(query, nzbPath).Scan(
		&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
		&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
		&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get queue item by path: %w", err)
	}

	return &item, nil
}

// RemoveFromQueue removes an item from the queue
func (r *Repository) RemoveFromQueue(id int64) error {
	query := `DELETE FROM import_queue WHERE id = ?`
	
	result, err := r.db.Exec(query, id)
	if err != nil {
		return fmt.Errorf("failed to remove from queue: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("queue item not found")
	}

	return nil
}

// GetQueueStats retrieves current queue statistics
func (r *Repository) GetQueueStats() (*QueueStats, error) {
	// Update stats from actual queue data
	err := r.UpdateQueueStats()
	if err != nil {
		return nil, fmt.Errorf("failed to update queue stats: %w", err)
	}

	query := `
		SELECT id, total_queued, total_processing, total_completed, total_failed, 
		       avg_processing_time_ms, last_updated
		FROM queue_stats ORDER BY id DESC LIMIT 1
	`

	var stats QueueStats
	err = r.db.QueryRow(query).Scan(
		&stats.ID, &stats.TotalQueued, &stats.TotalProcessing, &stats.TotalCompleted,
		&stats.TotalFailed, &stats.AvgProcessingTimeMs, &stats.LastUpdated,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			// Initialize default stats if none exist
			defaultStats := &QueueStats{
				TotalQueued:     0,
				TotalProcessing: 0,
				TotalCompleted:  0,
				TotalFailed:     0,
				LastUpdated:     time.Now(),
			}
			return defaultStats, nil
		}
		return nil, fmt.Errorf("failed to get queue stats: %w", err)
	}

	return &stats, nil
}

// UpdateQueueStats updates queue statistics based on current queue state
func (r *Repository) UpdateQueueStats() error {
	// Get current counts
	countQueries := []string{
		`SELECT COUNT(*) FROM import_queue WHERE status IN ('pending', 'retrying')`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'processing'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'completed'`,
		`SELECT COUNT(*) FROM import_queue WHERE status = 'failed'`,
	}

	var counts [4]int
	for i, query := range countQueries {
		err := r.db.QueryRow(query).Scan(&counts[i])
		if err != nil {
			return fmt.Errorf("failed to get count for query %d: %w", i, err)
		}
	}

	// Calculate average processing time for completed items
	var avgProcessingTime sql.NullInt64
	avgQuery := `
		SELECT AVG(CAST((julianday(completed_at) - julianday(started_at)) * 24 * 60 * 60 * 1000 AS INTEGER))
		FROM import_queue 
		WHERE status = 'completed' AND started_at IS NOT NULL AND completed_at IS NOT NULL
	`
	err := r.db.QueryRow(avgQuery).Scan(&avgProcessingTime)
	if err != nil {
		return fmt.Errorf("failed to calculate average processing time: %w", err)
	}

	// Update or insert stats
	updateQuery := `
		UPDATE queue_stats SET 
		total_queued = ?, total_processing = ?, total_completed = ?, total_failed = ?,
		avg_processing_time_ms = ?, last_updated = ?
		WHERE id = (SELECT MAX(id) FROM queue_stats)
	`

	var avgTime interface{}
	if avgProcessingTime.Valid {
		avgTime = avgProcessingTime.Int64
	} else {
		avgTime = nil
	}

	_, err = r.db.Exec(updateQuery, counts[0], counts[1], counts[2], counts[3], avgTime, time.Now())
	if err != nil {
		return fmt.Errorf("failed to update queue stats: %w", err)
	}

	return nil
}

// ListQueueItems retrieves queue items with optional filtering
func (r *Repository) ListQueueItems(status *QueueStatus, limit, offset int) ([]*ImportQueueItem, error) {
	var query string
	var args []interface{}

	if status != nil {
		query = `
			SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
			FROM import_queue WHERE status = ?
			ORDER BY priority ASC, created_at ASC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{*status, limit, offset}
	} else {
		query = `
			SELECT id, nzb_path, watch_root, priority, status, created_at, updated_at,
			       started_at, completed_at, retry_count, max_retries, error_message, batch_id, metadata
			FROM import_queue 
			ORDER BY priority ASC, created_at ASC
			LIMIT ? OFFSET ?
		`
		args = []interface{}{limit, offset}
	}

	rows, err := r.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list queue items: %w", err)
	}
	defer rows.Close()

	var items []*ImportQueueItem
	for rows.Next() {
		var item ImportQueueItem
		err := rows.Scan(
			&item.ID, &item.NzbPath, &item.WatchRoot, &item.Priority, &item.Status,
			&item.CreatedAt, &item.UpdatedAt, &item.StartedAt, &item.CompletedAt,
			&item.RetryCount, &item.MaxRetries, &item.ErrorMessage, &item.BatchID, &item.Metadata,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan queue item: %w", err)
		}
		items = append(items, &item)
	}

	return items, rows.Err()
}

// ClearCompletedQueueItems removes completed and failed items from the queue
func (r *Repository) ClearCompletedQueueItems(olderThan time.Time) (int, error) {
	query := `
		DELETE FROM import_queue 
		WHERE status IN ('completed', 'failed') AND updated_at < ?
	`

	result, err := r.db.Exec(query, olderThan)
	if err != nil {
		return 0, fmt.Errorf("failed to clear completed queue items: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return int(rowsAffected), nil
}
