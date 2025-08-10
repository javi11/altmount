package database

import (
	"database/sql"
	"fmt"
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
		INSERT INTO nzb_files (path, filename, size, nzb_type, segments_count, segments_data, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`
	
	result, err := r.db.Exec(query, nzbFile.Path, nzbFile.Filename, nzbFile.Size, 
		nzbFile.NzbType, nzbFile.SegmentsCount, nzbFile.SegmentsData, time.Now())
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
		SELECT id, path, filename, size, created_at, updated_at, nzb_type, segments_count, segments_data
		FROM nzb_files WHERE path = ?
	`
	
	var nzbFile NzbFile
	err := r.db.QueryRow(query, path).Scan(
		&nzbFile.ID, &nzbFile.Path, &nzbFile.Filename, &nzbFile.Size,
		&nzbFile.CreatedAt, &nzbFile.UpdatedAt, &nzbFile.NzbType,
		&nzbFile.SegmentsCount, &nzbFile.SegmentsData,
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
		INSERT INTO virtual_files (nzb_file_id, virtual_path, filename, size, is_directory, parent_path)
		VALUES (?, ?, ?, ?, ?, ?)
	`
	
	result, err := r.db.Exec(query, vf.NzbFileID, vf.VirtualPath, vf.Filename, 
		vf.Size, vf.IsDirectory, vf.ParentPath)
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
		SELECT id, nzb_file_id, virtual_path, filename, size, created_at, is_directory, parent_path
		FROM virtual_files WHERE virtual_path = ?
	`
	
	var vf VirtualFile
	err := r.db.QueryRow(query, path).Scan(
		&vf.ID, &vf.NzbFileID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.ParentPath,
	)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get virtual file: %w", err)
	}
	
	return &vf, nil
}

// ListVirtualFilesByParentPath retrieves virtual files under a parent directory
func (r *Repository) ListVirtualFilesByParentPath(parentPath string) ([]*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, virtual_path, filename, size, created_at, is_directory, parent_path
		FROM virtual_files WHERE parent_path = ? ORDER BY is_directory DESC, filename ASC
	`
	
	rows, err := r.db.Query(query, parentPath)
	if err != nil {
		return nil, fmt.Errorf("failed to list virtual files: %w", err)
	}
	defer rows.Close()
	
	var files []*VirtualFile
	for rows.Next() {
		var vf VirtualFile
		err := rows.Scan(
			&vf.ID, &vf.NzbFileID, &vf.VirtualPath, &vf.Filename,
			&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.ParentPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan virtual file: %w", err)
		}
		files = append(files, &vf)
	}
	
	return files, rows.Err()
}

// GetVirtualFileWithNzb retrieves a virtual file along with its NZB file data
func (r *Repository) GetVirtualFileWithNzb(path string) (*VirtualFile, *NzbFile, error) {
	query := `
		SELECT vf.id, vf.nzb_file_id, vf.virtual_path, vf.filename, vf.size, vf.created_at, vf.is_directory, vf.parent_path,
		       nf.id, nf.path, nf.filename, nf.size, nf.created_at, nf.updated_at, nf.nzb_type, nf.segments_count, nf.segments_data
		FROM virtual_files vf
		JOIN nzb_files nf ON vf.nzb_file_id = nf.id
		WHERE vf.virtual_path = ?
	`
	
	var vf VirtualFile
	var nf NzbFile
	
	err := r.db.QueryRow(query, path).Scan(
		&vf.ID, &vf.NzbFileID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.ParentPath,
		&nf.ID, &nf.Path, &nf.Filename, &nf.Size,
		&nf.CreatedAt, &nf.UpdatedAt, &nf.NzbType,
		&nf.SegmentsCount, &nf.SegmentsData,
	)
	
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("failed to get virtual file with nzb: %w", err)
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
func (r *Repository) GetDirectoryTree() (map[string][]*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, virtual_path, filename, size, created_at, is_directory, parent_path
		FROM virtual_files ORDER BY parent_path, is_directory DESC, filename ASC
	`
	
	rows, err := r.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to get directory tree: %w", err)
	}
	defer rows.Close()
	
	tree := make(map[string][]*VirtualFile)
	
	for rows.Next() {
		var vf VirtualFile
		err := rows.Scan(
			&vf.ID, &vf.NzbFileID, &vf.VirtualPath, &vf.Filename,
			&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.ParentPath,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan virtual file: %w", err)
		}
		
		parentPath := ""
		if vf.ParentPath != nil {
			parentPath = *vf.ParentPath
		}
		
		tree[parentPath] = append(tree[parentPath], &vf)
	}
	
	return tree, rows.Err()
}

// GetVirtualFile retrieves a virtual file by its ID
func (r *Repository) GetVirtualFile(id int64) (*VirtualFile, error) {
	query := `
		SELECT id, nzb_file_id, virtual_path, filename, size, created_at, is_directory, parent_path
		FROM virtual_files WHERE id = ?
	`
	
	var vf VirtualFile
	err := r.db.QueryRow(query, id).Scan(
		&vf.ID, &vf.NzbFileID, &vf.VirtualPath, &vf.Filename,
		&vf.Size, &vf.CreatedAt, &vf.IsDirectory, &vf.ParentPath,
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