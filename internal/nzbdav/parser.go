package nzbdav

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"text/template"

	_ "github.com/mattn/go-sqlite3"
)

type Parser struct {
	dbPath string
}

func NewParser(dbPath string) *Parser {
	return &Parser{dbPath: dbPath}
}

// Parse streams NZBs from the database
func (p *Parser) Parse() (<-chan *ParsedNzb, <-chan error) {
	out := make(chan *ParsedNzb)
	errChan := make(chan error, 1)

	go func() {
		defer close(out)
		defer close(errChan)

		db, err := sql.Open("sqlite3", p.dbPath)
		if err != nil {
			errChan <- fmt.Errorf("failed to open database: %w", err)
			return
		}
		defer db.Close()

		// Query to find all "Release" folders
		// A release folder is a parent of any item that has an entry in DavNzbFiles
		rows, err := db.Query(`
			SELECT DISTINCT p.Id, p.Name, p.Path 
			FROM DavItems p
			JOIN DavItems c ON c.ParentId = p.Id
			JOIN DavNzbFiles n ON n.Id = c.Id
		`)
		if err != nil {
			errChan <- fmt.Errorf("failed to query releases: %w", err)
			return
		}
		defer rows.Close()

		for rows.Next() {
			var id, name, path string
			if err := rows.Scan(&id, &name, &path); err != nil {
				slog.Error("Failed to scan release row", "error", err)
				continue
			}

			// Create pipe for streaming content
			pr, pw := io.Pipe()

			// Launch goroutine to write NZB content to the pipe
			// We use a WaitGroup to ensure we don't close the DB before this goroutine finishes? 
			// No, the outer loop waits for rows.Next(), but we launch a goroutine.
			// Actually, if we launch 'go p.writeNzb', the outer loop continues.
			// BUT, the channel send 'out <- ...' blocks until the receiver takes it.
			// The receiver (handler) reads the 'Content' (pipe) fully before taking the next item?
			// Yes, 'io.Copy(outFile, res.Content)' blocks until EOF.
			// So 'writeNzb' will finish *before* the handler takes the *next* item from 'out'? 
			// No, 'io.Copy' reads from 'pr'. 'writeNzb' writes to 'pw'.
			// The handler calls 'io.Copy', which drains 'pr'.
			// Once 'pr' is drained (EOF), the handler loop continues and might read from 'out' again.
			// So we are effectively serializing the DB access mostly, EXCEPT that 'writeNzb' runs concurrently with 'io.Copy'.
			// But since we are inside the 'rows.Next()' loop, we can't easily pass 'db' to 'writeNzb' if we want to query concurrently 
			// because sqlite3 has locking (though read-only is usually fine with WAL, but default might be strict).
			// Ideally, we shouldn't share the 'db' connection object across goroutines if we are iterating 'rows'.
			// Actually, 'database/sql' is thread-safe. But iterating 'rows' holds a connection.
			// If 'writeNzb' tries to query, it might need another connection from the pool.
			// This should be fine.
			
			go p.writeNzb(p.dbPath, id, name, pw)

			category := p.deriveCategory(path)
			select {
			case out <- &ParsedNzb{
				Name:     name,
				Category: category,
				RelPath:  p.deriveRelPath(path, category),
				Content:  pr,
			}:
			case <-errChan: // Should not happen given logic, but good practice
				return
			}
		}
	}()

	return out, errChan
}

func (p *Parser) deriveCategory(path string) string {
	lowerPath := strings.ToLower(path)
	if strings.Contains(lowerPath, "/movies/") || strings.Contains(lowerPath, "/movie/") {
		return "movies"
	}
	if strings.Contains(lowerPath, "/tv/") || strings.Contains(lowerPath, "/series/") {
		return "tv"
	}
	return "other"
}

func (p *Parser) deriveRelPath(path, category string) string {
	// 1. Clean path separators
	path = strings.ReplaceAll(path, "\\", "/")
	path = strings.Trim(path, "/")

	// 2. Identify and remove category prefix
	// We want to find the category folder in the path and return everything after it
	// e.g. /content/tv/Show/Season 1 -> Show/Season 1
	parts := strings.Split(path, "/")
	
	// Remove the last part (Release Name) as that is handled by the release name itself
	if len(parts) > 0 {
		parts = parts[:len(parts)-1]
	}

	// Find where the category folder is
	categoryIndex := -1
	for i, part := range parts {
		lowerPart := strings.ToLower(part)
		if lowerPart == category || (category == "tv" && lowerPart == "series") || (category == "movies" && lowerPart == "movie") {
			categoryIndex = i
			break
		}
	}

	if categoryIndex != -1 && categoryIndex < len(parts)-1 {
		// Return everything AFTER the category
		return strings.Join(parts[categoryIndex+1:], "/")
	}

	// If category not found or it's the last folder, return empty
	// This prevents returning "content/tv" which results in "tv/content/tv"
	return ""
}

// writeNzb generates the NZB XML and writes it to the pipe
// It opens its own DB connection to avoid interfering with the main iteration loop
func (p *Parser) writeNzb(dbPath, releaseId, releaseName string, pw *io.PipeWriter) {
	defer pw.Close()

	// We open a new connection for this operation to ensure thread safety and avoid lock contention
	// especially since the main loop is holding a cursor open.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("failed to open db for nzb generation: %w", err))
		return
	}
	defer db.Close()

	// Fetch files for this release
	rows, err := db.Query(`
		SELECT c.Name, c.FileSize, n.SegmentIds
		FROM DavItems c
		JOIN DavNzbFiles n ON n.Id = c.Id
		WHERE c.ParentId = ?
	`, releaseId)
	if err != nil {
		pw.CloseWithError(fmt.Errorf("failed to query files: %w", err))
		return
	}
	defer rows.Close()

	// Start writing NZB Header
	header := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
	<head>
		<meta type="name">` + releaseName + `</meta>
	</head>
`
	if _, err := pw.Write([]byte(header)); err != nil {
		return
	}

	// Iterate files and write segments
	for rows.Next() {
		var fileName string
		var fileSize sql.NullInt64
		var segmentIdsJSON string

		if err := rows.Scan(&fileName, &fileSize, &segmentIdsJSON); err != nil {
			slog.Error("Failed to scan file row", "error", err)
			continue
		}

		var segmentIds []string
		if err := json.Unmarshal([]byte(segmentIdsJSON), &segmentIds); err != nil {
			slog.Error("Failed to unmarshal segment IDs", "file", fileName, "error", err)
			continue
		}

		if len(segmentIds) == 0 {
			continue
		}

		// Calculate segment size
		totalBytes := int64(0)
		if fileSize.Valid {
			totalBytes = fileSize.Int64
		}
		
		// Estimate bytes per segment
		bytesPerSegment := int64(0)
		if totalBytes > 0 {
			bytesPerSegment = totalBytes / int64(len(segmentIds))
		}

		// Write File Header
		fileHeader := fmt.Sprintf(`	<file poster="AltMount" date="%d" subject="%s">
		<groups>
			<group>alt.binaries.test</group>
		</groups>
		<segments>
`, 0, template.HTMLEscapeString(fileName)) 
		// Note: Date is 0 as we don't have it easily available, subject is filename

		if _, err := pw.Write([]byte(fileHeader)); err != nil {
			return
		}

		// Write Segments
		for i, msgId := range segmentIds {
			segBytes := bytesPerSegment
			// Adjust last segment size
			if i == len(segmentIds)-1 && totalBytes > 0 {
				segBytes = totalBytes - (bytesPerSegment * int64(i))
			}
			if segBytes <= 0 {
				segBytes = 1 // Fallback
			}

			segmentLine := fmt.Sprintf(`			<segment bytes="%d" number="%d">%s</segment>
`, segBytes, i+1, template.HTMLEscapeString(msgId))
			
			if _, err := pw.Write([]byte(segmentLine)); err != nil {
				return
			}
		}

		// Write File Footer
		if _, err := pw.Write([]byte(`		</segments>
	</file>
`)); err != nil {
			return
		}
	}

	// Write NZB Footer
	if _, err := pw.Write([]byte(`</nzb>`)); err != nil {
		return
	}
}
