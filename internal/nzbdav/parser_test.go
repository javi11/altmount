package nzbdav

import (
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openLegacyDB creates an in-memory-ish temp DB with the minimal schema used
// by the legacy reconstruction path.
func openLegacyDB(t *testing.T) (*sql.DB, string) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY,
			ParentId TEXT,
			Name TEXT,
			FileSize INTEGER,
			Type INTEGER,
			Path TEXT
		);
		CREATE TABLE DavNzbFiles (
			Id TEXT PRIMARY KEY,
			SegmentIds TEXT
		);
	`)
	require.NoError(t, err)
	return db, dbPath
}

func collect(t *testing.T, out <-chan *ParsedNzb, errChan <-chan error) []*ParsedNzb {
	t.Helper()
	var got []*ParsedNzb
	for {
		select {
		case res, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, res)
		case err, ok := <-errChan:
			if ok && err != nil {
				t.Fatalf("parser error: %v", err)
			}
		}
	}
}

func TestParser_Parse_MergesExtractedIntoRelease(t *testing.T) {
	db, dbPath := openLegacyDB(t)

	_, err := db.Exec(`
		INSERT INTO DavItems (Id, ParentId, Name, FileSize, Type, Path) VALUES
		('root',    NULL,     '/',                  NULL,      1, '/'),
		('movies',  'root',   'movies',             NULL,      1, '/movies'),
		('rel1',    'movies', 'My.Release.1080p',   NULL,      1, '/movies/My.Release.1080p'),
		('file1',   'rel1',   'movie.mkv',          1048576,   0, '/movies/My.Release.1080p/movie.mkv'),
		('rel2',    'movies', 'Actual.Movie.Name',  NULL,      1, '/movies/Actual.Movie.Name'),
		('ext',     'rel2',   'extracted',          NULL,      1, '/movies/Actual.Movie.Name/extracted'),
		('fileMain','rel2',   'movie2.nzb',         2097152,   0, '/movies/Actual.Movie.Name/movie2.nzb'),
		('fileExt', 'ext',    'movie2.mkv',         2097152,   0, '/movies/Actual.Movie.Name/extracted/movie2.mkv');

		INSERT INTO DavNzbFiles (Id, SegmentIds) VALUES
		('file1',    '["msg1@test","msg2@test"]'),
		('fileMain', '["msg3@test"]'),
		('fileExt',  '["msg4@test"]');
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, "").Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 2)

	byName := map[string]*ParsedNzb{}
	for _, r := range got {
		byName[r.Name] = r
	}

	rel1 := byName["My.Release.1080p"]
	require.NotNil(t, rel1)
	assert.Equal(t, "movies", rel1.Category)
	assert.Empty(t, rel1.ExtractedFiles)
	rel1Body, _ := io.ReadAll(rel1.Content)
	assert.Contains(t, string(rel1Body), `<meta type="name">My.Release.1080p</meta>`)
	assert.Contains(t, string(rel1Body), "NZBDAV_ID:file1")
	assert.Contains(t, string(rel1Body), `poster="nzbdav"`)
	assert.NotContains(t, string(rel1Body), "alt.binaries.test")

	rel2 := byName["Actual.Movie.Name"]
	require.NotNil(t, rel2)
	assert.Equal(t, "movies", rel2.Category)
	rel2Body, _ := io.ReadAll(rel2.Content)
	assert.Contains(t, string(rel2Body), `<meta type="name">Actual.Movie.Name</meta>`)
	// Primary file (not in /extracted) must be in the NZB body.
	assert.Contains(t, string(rel2Body), "NZBDAV_ID:fileMain")
	// File inside /extracted/ must NOT appear as a <file> entry — it's metadata only.
	assert.NotContains(t, string(rel2Body), "NZBDAV_ID:fileExt")

	// Extracted files list is populated from the /extracted subtree.
	require.Len(t, rel2.ExtractedFiles, 1)
	assert.Equal(t, "movie2.mkv", rel2.ExtractedFiles[0].Name)
	assert.Equal(t, int64(2097152), rel2.ExtractedFiles[0].Size)
}

func TestParser_Parse_TVCategory(t *testing.T) {
	db, dbPath := openLegacyDB(t)

	_, err := db.Exec(`
		INSERT INTO DavItems (Id, ParentId, Name, FileSize, Type, Path) VALUES
		('root', NULL,   '/',                   NULL,    1, '/'),
		('tv',   'root', 'tv',                  NULL,    1, '/tv'),
		('show', 'tv',   'Show.S01.1080p',      NULL,    1, '/tv/Show.S01.1080p'),
		('ep1',  'show', 'Show.S01E01.mkv',     1048576, 0, '/tv/Show.S01.1080p/Show.S01E01.mkv');

		INSERT INTO DavNzbFiles (Id, SegmentIds) VALUES
		('ep1', '["seg1@test"]');
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, "").Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	assert.Equal(t, "tv", got[0].Category)
	assert.Equal(t, "Show.S01.1080p", got[0].Name)
}

func TestParser_Parse_MissingFileSize(t *testing.T) {
	db, dbPath := openLegacyDB(t)

	_, err := db.Exec(`
		INSERT INTO DavItems (Id, ParentId, Name, FileSize, Type, Path) VALUES
		('root',   NULL,     '/',                 NULL, 1, '/'),
		('movies', 'root',   'movies',            NULL, 1, '/movies'),
		('rel',    'movies', 'NoSize.Release',    NULL, 1, '/movies/NoSize.Release'),
		('file',   'rel',    'thing.mkv',         NULL, 0, '/movies/NoSize.Release/thing.mkv');

		INSERT INTO DavNzbFiles (Id, SegmentIds) VALUES
		('file', '["m1@t","m2@t"]');
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, "").Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	body, _ := io.ReadAll(got[0].Content)
	s := string(body)
	// Both segments should use the 750_000-byte default, never 1.
	assert.Equal(t, 2, strings.Count(s, `bytes="750000"`), "expected both segments to fall back to default size: %s", s)
	assert.NotContains(t, s, `bytes="1"`)
}

func writeZstdBlob(t *testing.T, path string, content []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	zw, err := zstd.NewWriter(f)
	require.NoError(t, err)
	_, err = zw.Write(content)
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, f.Close())
}

func TestParser_Parse_Blobs(t *testing.T) {
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "blobs.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY,
			ParentId TEXT,
			Name TEXT,
			FileSize INTEGER,
			Path TEXT,
			NzbBlobId TEXT,
			SubType INTEGER
		);
		CREATE TABLE NzbNames (
			Id TEXT PRIMARY KEY,
			FileName TEXT
		);
	`)
	require.NoError(t, err)

	blobId := "a1b2c3d4e5f6g7h8"
	blobPath := filepath.Join(blobsDir, blobId[0:2], blobId[2:4], blobId)
	nzbContent := `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
	<file poster="poster" date="12345" subject="subject">
		<groups><group>alt.binaries.test</group></groups>
		<segments><segment bytes="100" number="1">msgid@test</segment></segments>
	</file>
</nzb>`
	writeZstdBlob(t, blobPath, []byte(nzbContent))

	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES ('a1b2c3d4e5f6g7h8', 'My Movie.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, Path, NzbBlobId, SubType) VALUES
		('root',   NULL,      '/',                      '/',                              NULL,               1),
		('movies', 'root',    'movies',                 '/movies',                        NULL,               1),
		('folder', 'movies',  'My Movie',               '/movies/My Movie',               NULL,               1),
		('item1',  'folder',  'My Movie.mkv',           '/movies/My Movie/My Movie.mkv',  'a1b2c3d4e5f6g7h8', 203);
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	assert.Equal(t, "item1", got[0].ID)
	assert.Equal(t, "My Movie", got[0].Name)
	assert.Equal(t, "movies", got[0].Category)
	body, _ := io.ReadAll(got[0].Content)
	assert.Equal(t, nzbContent, string(body))
}

// TestParser_Parse_Blobs_UppercaseUUID verifies that blob IDs stored uppercase in
// the SQLite database (real nzbdav format, e.g. "0AA2BD24-B90C-4E06-A301-DD0D296AD86C")
// are matched against their lowercase on-disk layout, since nzbdav's C# BlobStore
// writes paths using Guid.ToString("N") which is always lowercase.
func TestParser_Parse_Blobs_UppercaseUUID(t *testing.T) {
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "blobs_upper.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY,
			ParentId TEXT,
			Name TEXT,
			FileSize INTEGER,
			Path TEXT,
			NzbBlobId TEXT,
			SubType INTEGER
		);
		CREATE TABLE NzbNames (
			Id TEXT PRIMARY KEY,
			FileName TEXT
		);
	`)
	require.NoError(t, err)

	// DB stores the UUID uppercase with hyphens (default EF Core Guid TEXT format).
	dbBlobID := "0AA2BD24-B90C-4E06-A301-DD0D296AD86C"
	// Disk stores it lowercase (Guid.ToString("N") / Guid.ToString()).
	diskBlobID := "0aa2bd24-b90c-4e06-a301-dd0d296ad86c"
	blobPath := filepath.Join(blobsDir, diskBlobID[0:2], diskBlobID[2:4], diskBlobID)
	nzbContent := `<?xml version="1.0" encoding="UTF-8"?>
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
	<file poster="poster" date="12345" subject="subject">
		<groups><group>alt.binaries.test</group></groups>
		<segments><segment bytes="100" number="1">msgid@test</segment></segments>
	</file>
</nzb>`
	writeZstdBlob(t, blobPath, []byte(nzbContent))

	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES (?, 'My Movie.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, Path, NzbBlobId, SubType) VALUES
		('root',   NULL,      '/',                     '/',                             NULL, 1),
		('movies', 'root',    'movies',                '/movies',                       NULL, 1),
		('folder', 'movies',  'My Movie',              '/movies/My Movie',              NULL, 1),
		('item1',  'folder',  'My Movie.mkv',          '/movies/My Movie/My Movie.mkv', ?,    203);
	`, dbBlobID, dbBlobID)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	assert.Equal(t, "item1", got[0].ID)
	body, _ := io.ReadAll(got[0].Content)
	assert.Equal(t, nzbContent, string(body))
}

func TestParser_Parse_Blobs_Uncompressed(t *testing.T) {
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "blobs_plain.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY, ParentId TEXT, Name TEXT, FileSize INTEGER,
			Path TEXT, NzbBlobId TEXT, SubType INTEGER
		);
		CREATE TABLE NzbNames (Id TEXT PRIMARY KEY, FileName TEXT);
	`)
	require.NoError(t, err)

	blobId := "ff00ff00ff00ff00"
	plain := `<?xml version="1.0" encoding="UTF-8"?><nzb><file subject="x"/></nzb>`
	blobPath := filepath.Join(blobsDir, blobId[0:2], blobId[2:4], blobId)
	require.NoError(t, os.MkdirAll(filepath.Dir(blobPath), 0o755))
	require.NoError(t, os.WriteFile(blobPath, []byte(plain), 0o644))

	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES ('ff00ff00ff00ff00', 'Thing.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, Path, NzbBlobId, SubType) VALUES
		('root',   NULL,     '/',              '/',                       NULL, 1),
		('movies', 'root',   'movies',         '/movies',                 NULL, 1),
		('folder', 'movies', 'Thing',          '/movies/Thing',           NULL, 1),
		('item',   'folder', 'Thing.mkv',      '/movies/Thing/Thing.mkv', 'ff00ff00ff00ff00', 203);
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	body, err := io.ReadAll(got[0].Content)
	require.NoError(t, err)
	assert.Equal(t, plain, string(body))
}

func TestParser_Parse_Blobs_PreservesArbitraryFolderStructure(t *testing.T) {
	// Mirrors the real-world nzbdav layout /content/uncategorized/<release>/<file>
	// reported by users. The parser must preserve this tree verbatim instead of
	// forcing /movies/, /tv/, or /other/.
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "content.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY, ParentId TEXT, Name TEXT, FileSize INTEGER,
			Path TEXT, NzbBlobId TEXT, SubType INTEGER
		);
		CREATE TABLE NzbNames (Id TEXT PRIMARY KEY, FileName TEXT);
	`)
	require.NoError(t, err)

	blobId := "1234567890abcdef"
	writeZstdBlob(t, filepath.Join(blobsDir, "12", "34", blobId), []byte("<nzb/>"))

	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES ('1234567890abcdef', 'Fresh.Off.The.Boat.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, Path, NzbBlobId, SubType) VALUES
		('root',    NULL,    '/',                     '/',                                                      NULL, 1),
		('content', 'root',  'content',               '/content',                                               NULL, 1),
		('uncat',   'content','uncategorized',        '/content/uncategorized',                                 NULL, 1),
		('folder',  'uncat', 'Fresh.Off.The.Boat',    '/content/uncategorized/Fresh.Off.The.Boat',              NULL, 1),
		('item',    'folder','Fresh.Off.The.Boat.mkv','/content/uncategorized/Fresh.Off.The.Boat/Fresh.Off.The.Boat.mkv', '1234567890abcdef', 203);
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	assert.Equal(t, "Fresh.Off.The.Boat", got[0].Name)
	assert.Equal(t, "content", got[0].Category)
	assert.Equal(t, "uncategorized", got[0].RelPath)
}

// TestParser_Parse_Blobs_DeduplicateNestedSubType203 reproduces the real-world
// nzbdav bug where a release with many subfiles produces two SubType=203
// DavItems (e.g. RUNE release): one as a direct child of the release folder,
// and a second nested under the first. Without deduplication both would be
// emitted as separate ParsedNzb values, causing a double import.
func TestParser_Parse_Blobs_DeduplicateNestedSubType203(t *testing.T) {
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "dedup.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY, ParentId TEXT, Name TEXT, FileSize INTEGER,
			Path TEXT, NzbBlobId TEXT, SubType INTEGER
		);
		CREATE TABLE NzbNames (Id TEXT PRIMARY KEY, FileName TEXT);
	`)
	require.NoError(t, err)

	blobId := "aabbccddee001122"
	writeZstdBlob(t, filepath.Join(blobsDir, "aa", "bb", blobId), []byte("<nzb/>"))

	// Two SubType=203 rows for the same release/blob — the second is nested
	// under the first, mirroring the malformed nzbdav DB structure.
	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES ('aabbccddee001122', 'Big.Release.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, Path, NzbBlobId, SubType) VALUES
		('root',    NULL,      '/',                                 '/',                                    NULL,               1),
		('uncat',   'root',    'uncategorized',                     '/uncategorized',                       NULL,               1),
		('folder',  'uncat',   'Big.Release',                       '/uncategorized/Big.Release',           NULL,               1),
		('item1',   'folder',  'Big.Release',                       '/uncategorized/Big.Release/Big.Release',           'aabbccddee001122', 203),
		('item2',   'item1',   'Big.Release',                       '/uncategorized/Big.Release/Big.Release/Big.Release','aabbccddee001122', 203);
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1, "expected exactly one ParsedNzb despite two SubType=203 rows for the same release")
	assert.Equal(t, "Big.Release", got[0].Name)
}

func TestParser_Parse_Blobs_WithExtracted(t *testing.T) {
	tmpDir := t.TempDir()
	blobsDir := filepath.Join(tmpDir, "blobs")
	dbPath := filepath.Join(tmpDir, "blobs_ext.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE DavItems (
			Id TEXT PRIMARY KEY,
			ParentId TEXT,
			Name TEXT,
			FileSize INTEGER,
			Path TEXT,
			NzbBlobId TEXT,
			SubType INTEGER
		);
		CREATE TABLE NzbNames (
			Id TEXT PRIMARY KEY,
			FileName TEXT
		);
	`)
	require.NoError(t, err)

	blobId := "0011223344556677"
	writeZstdBlob(t, filepath.Join(blobsDir, "00", "11", blobId), []byte("<nzb/>"))

	_, err = db.Exec(`
		INSERT INTO NzbNames (Id, FileName) VALUES ('0011223344556677', 'Movie.nzb');
		INSERT INTO DavItems (Id, ParentId, Name, FileSize, Path, NzbBlobId, SubType) VALUES
		('root',    NULL,     '/',              NULL,    '/',                                       NULL,               1),
		('movies',  'root',   'movies',         NULL,    '/movies',                                 NULL,               1),
		('folder',  'movies', 'Movie',          NULL,    '/movies/Movie',                           NULL,               1),
		('item',    'folder', 'Movie.mkv',      NULL,    '/movies/Movie/Movie.mkv',                 '0011223344556677', 203),
		('ext',     'folder', 'extracted',      NULL,    '/movies/Movie/extracted',                 NULL,               1),
		('inner',   'ext',    'feature.mkv',    5242880, '/movies/Movie/extracted/feature.mkv',     NULL,               0);
	`)
	require.NoError(t, err)

	out, errChan := NewParser(dbPath, blobsDir).Parse()
	got := collect(t, out, errChan)

	require.Len(t, got, 1)
	require.Len(t, got[0].ExtractedFiles, 1)
	assert.Equal(t, "feature.mkv", got[0].ExtractedFiles[0].Name)
	assert.Equal(t, int64(5242880), got[0].ExtractedFiles[0].Size)
}
