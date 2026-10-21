package database

import "context"

// DuplicateCandidate is a local metadata version and its lazily computed fingerprint.
type DuplicateCandidate struct {
	Path, Name     string
	Size           int64
	Identity, Hash string
}

func (r *HealthRepository) UpsertDuplicateCandidate(ctx context.Context, c DuplicateCandidate) error {
	_, e := r.db.ExecContext(ctx, `INSERT INTO duplicate_lookup_catalog(file_path,original_name,file_size,identity,fingerprint) VALUES(?,?,?,?,?) ON CONFLICT(file_path) DO UPDATE SET original_name=excluded.original_name,file_size=excluded.file_size,fingerprint=CASE WHEN duplicate_lookup_catalog.identity=excluded.identity THEN duplicate_lookup_catalog.fingerprint ELSE '' END,identity=excluded.identity`, c.Path, c.Name, c.Size, c.Identity, c.Hash)
	return e
}
func (r *HealthRepository) DuplicateCandidates(ctx context.Context, name string, size int64) ([]DuplicateCandidate, error) {
	rows, e := r.db.QueryContext(ctx, `SELECT file_path,original_name,file_size,identity,fingerprint FROM duplicate_lookup_catalog WHERE original_name=? AND file_size=? ORDER BY file_path`, name, size)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	var out []DuplicateCandidate
	for rows.Next() {
		var c DuplicateCandidate
		if e = rows.Scan(&c.Path, &c.Name, &c.Size, &c.Identity, &c.Hash); e != nil {
			return nil, e
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
func (r *HealthRepository) SetDuplicateFingerprint(ctx context.Context, path, identity, hash string) error {
	_, e := r.db.ExecContext(ctx, `UPDATE duplicate_lookup_catalog SET fingerprint=? WHERE file_path=? AND identity=?`, hash, path, identity)
	return e
}
func (r *HealthRepository) DeleteDuplicateCandidate(ctx context.Context, path string) error {
	_, e := r.db.ExecContext(ctx, `DELETE FROM duplicate_lookup_catalog WHERE file_path=?`, path)
	return e
}
