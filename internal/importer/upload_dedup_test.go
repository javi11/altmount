package importer

import (
	"context"
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/require"
)

const dedupNzbContent = `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/2003/nzb">
  <file poster="x@x" date="1700000000" subject="Movie.bin (1/1)">
    <groups><group>alt.binaries.test</group></groups>
    <segments><segment bytes="100" number="1">abc123@example</segment></segments>
  </file>
</nzb>
`

func newDedupTestService(t *testing.T) (*Service, string) {
	t.Helper()
	configDir := t.TempDir()
	dbPath := filepath.Join(configDir, "altmount.db")

	db, err := database.NewDB(database.Config{Type: "sqlite", DatabasePath: dbPath})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	compressOff := false
	s := &Service{
		log:      slog.Default(),
		database: db,
		configGetter: func() *config.Config {
			return &config.Config{
				Database: config.DatabaseConfig{Path: dbPath},
				Import:   config.ImportConfig{CompressNzb: &compressOff},
			}
		},
	}
	return s, configDir
}

// uploadViaHandlerFlow mirrors handleUploadToQueue: de-dupe first, add only when there's no match.
func uploadViaHandlerFlow(t *testing.T, s *Service, uploadDir, filename string, category *string) *database.ImportQueueItem {
	t.Helper()
	prio := database.QueuePriorityNormal

	existing, err := s.FindAndUpdatePendingUpload(context.Background(), filename, category, &prio)
	require.NoError(t, err)
	if existing != nil {
		return existing
	}

	require.NoError(t, os.MkdirAll(uploadDir, 0o755))
	tempFile := filepath.Join(uploadDir, filename)
	require.NoError(t, os.WriteFile(tempFile, []byte(dedupNzbContent), 0o644))

	item, err := s.AddToQueue(context.Background(), tempFile, nil, category, &prio, nil, nil)
	require.NoError(t, err)
	return item
}

func countQueueRows(t *testing.T, dbPath string) []struct {
	id       int64
	category sql.NullString
} {
	t.Helper()
	read, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer read.Close()

	rows, err := read.Query(`SELECT id, category FROM import_queue ORDER BY id`)
	require.NoError(t, err)
	defer rows.Close()

	var out []struct {
		id       int64
		category sql.NullString
	}
	for rows.Next() {
		var r struct {
			id       int64
			category sql.NullString
		}
		require.NoError(t, rows.Scan(&r.id, &r.category))
		out = append(out, r)
	}
	return out
}

// TestReuploadSetsCategoryInPlace: re-uploading the same file to set a category must update the
// existing pending item, not create a duplicate.
func TestReuploadSetsCategoryInPlace(t *testing.T) {
	s, configDir := newDedupTestService(t)
	uploadDir := filepath.Join(configDir, "altmount-uploads")
	dbPath := filepath.Join(configDir, "altmount.db")

	first := uploadViaHandlerFlow(t, s, uploadDir, "Movie.nzb", nil)
	cat := "movies"
	second := uploadViaHandlerFlow(t, s, uploadDir, "Movie.nzb", &cat)

	require.Equal(t, first.ID, second.ID, "re-upload must reuse the existing queue item")
	require.NotNil(t, second.Category)
	require.Equal(t, "movies", *second.Category)

	rows := countQueueRows(t, dbPath)
	require.Len(t, rows, 1, "re-upload must not create a duplicate queue entry")
	require.Equal(t, "movies", rows[0].category.String)
}

// TestReuploadDifferentFileIsNotDeduped: distinct files must each get their own queue entry.
func TestReuploadDifferentFileIsNotDeduped(t *testing.T) {
	s, configDir := newDedupTestService(t)
	uploadDir := filepath.Join(configDir, "altmount-uploads")
	dbPath := filepath.Join(configDir, "altmount.db")

	cat := "movies"
	uploadViaHandlerFlow(t, s, uploadDir, "Movie.nzb", nil)
	uploadViaHandlerFlow(t, s, uploadDir, "Other.nzb", &cat)

	rows := countQueueRows(t, dbPath)
	require.Len(t, rows, 2, "distinct files must each get their own queue entry")
}
