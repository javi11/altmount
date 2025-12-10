package nzbdav

import (
	"database/sql"
	"io"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParser_Parse(t *testing.T) {
	// Create temp DB
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	require.NoError(t, err)
	defer db.Close()

	// Init Schema
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

	// Insert Data
	// Root -> Movies -> Release -> File
	_, err = db.Exec(`
		INSERT INTO DavItems (Id, ParentId, Name, Type, Path) VALUES 
		('root', NULL, '/', 1, '/'),
		('movies', 'root', 'movies', 1, '/movies'),
		('rel1', 'movies', 'My.Release.1080p', 1, '/movies/My.Release.1080p'),
		('file1', 'rel1', 'movie.mkv', 0, '/movies/My.Release.1080p/movie.mkv');

		INSERT INTO DavNzbFiles (Id, SegmentIds) VALUES 
		('file1', '["msg1@test", "msg2@test"]');
	`)
	require.NoError(t, err)

	// Run Parser
	parser := NewParser(dbPath)
	out, errChan := parser.Parse()

	// Verify
	select {
	case res, ok := <-out:
		require.True(t, ok)
		assert.Equal(t, "My.Release.1080p", res.Name)
		assert.Equal(t, "movies", res.Category)

		// Read content
		content, err := io.ReadAll(res.Content)
		require.NoError(t, err)
		xmlStr := string(content)

		assert.Contains(t, xmlStr, `<meta type="name">My.Release.1080p</meta>`)
		assert.Contains(t, xmlStr, `<file poster="AltMount"`)
		assert.Contains(t, xmlStr, `subject="movie.mkv">`)
		assert.Contains(t, xmlStr, `>msg1@test</segment>`)
		assert.Contains(t, xmlStr, `>msg2@test</segment>`)

	case err := <-errChan:
		require.NoError(t, err)
	}

	// Should be no more items
	_, ok := <-out
	assert.False(t, ok)
}
