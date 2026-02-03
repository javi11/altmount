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
		CREATE TABLE DavRarFiles (
			Id TEXT PRIMARY KEY,
			RarParts TEXT
		);
		CREATE TABLE DavMultipartFiles (
			Id TEXT PRIMARY KEY,
			Metadata TEXT
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
		('file1', 'rel1', 'movie.mkv', 0, '/movies/My.Release.1080p/movie.mkv'),
		('rel2', 'movies', 'Actual.Movie.Name', 1, '/movies/Actual.Movie.Name'),
		('ext', 'rel2', 'extracted', 1, '/movies/Actual.Movie.Name/extracted'),
		('file2', 'ext', 'movie2.mkv', 0, '/movies/Actual.Movie.Name/extracted/movie2.mkv');

		INSERT INTO DavNzbFiles (Id, SegmentIds) VALUES 
		('file1', '["msg1@test", "msg2@test"]'),
		('file2', '["msg3@test"]');
	`)
	require.NoError(t, err)

	// Run Parser
	parser := NewParser(dbPath)
	out, errChan := parser.Parse()

	// Verify
	// Note: ORDER BY c.ParentId, c.Name
	// file1 parent is rel1
	// file2 parent is ext

	// Item 1
	select {
	case res, ok := <-out:
		require.True(t, ok)
		// file2 (parent 'ext') might come first depending on sorting, but let's see.
		// Actually, since we order by ParentId, and IDs are likely UUIDs or sequential.
		// In our insert, 'ext' comes after 'rel1' alphabetically? maybe.

		if res.Name == "Actual.Movie.Name" {
			assert.Equal(t, "movies", res.Category)
			content, _ := io.ReadAll(res.Content)
			assert.Contains(t, string(content), `<meta type="name">Actual.Movie.Name</meta>`)
		} else {
			assert.Equal(t, "My.Release.1080p", res.Name)
			assert.Equal(t, "movies", res.Category)
			content, _ := io.ReadAll(res.Content)
			assert.Contains(t, string(content), `<meta type="name">My.Release.1080p</meta>`)
			assert.Contains(t, string(content), "subject=\"NZBDAV_ID:file1 &#34;movie.mkv&#34;\">")
		}

	case err := <-errChan:
		require.NoError(t, err)
	}

	// Item 2
	select {
	case res, ok := <-out:
		require.True(t, ok)
		if res.Name == "Actual.Movie.Name" {
			assert.Equal(t, "movies", res.Category)
			content, _ := io.ReadAll(res.Content)
			assert.Contains(t, string(content), `<meta type="name">Actual.Movie.Name</meta>`)
		} else {
			assert.Equal(t, "My.Release.1080p", res.Name)
			assert.Equal(t, "movies", res.Category)
			content, _ := io.ReadAll(res.Content)
			assert.Contains(t, string(content), `<meta type="name">My.Release.1080p</meta>`)
		}
	case err := <-errChan:
		require.NoError(t, err)
	}

	// Should be no more items
	_, ok := <-out
	assert.False(t, ok)
}
