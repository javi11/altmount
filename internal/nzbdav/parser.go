package nzbdav

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
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
		// WaitGroup to track active writers
		var wg sync.WaitGroup
		
		db, err := sql.Open("sqlite3", p.dbPath)
		if err != nil {
			errChan <- fmt.Errorf("failed to open database: %w", err)
			close(out)
			close(errChan)
			return
		}
		
		// Ensure DB is closed only after all writers are done
		defer func() {
			wg.Wait()
			db.Close()
			close(out)
			close(errChan)
		}()
		
		// Enable WAL mode for better concurrency if possible, 
		// but simple connection reuse is already a big win.
		// Set limits to prevent file descriptor exhaustion
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)

		// Query to find all "Release" folders
		// A release folder is a parent of any item that has an entry in DavNzbFiles or DavRarFiles
		rows, err := db.Query(`
			SELECT DISTINCT p.Id, p.Name, p.Path 
			FROM DavItems p
			JOIN DavItems c ON c.ParentId = p.Id
			LEFT JOIN DavNzbFiles n ON n.Id = c.Id
			LEFT JOIN DavRarFiles r ON r.Id = c.Id
			WHERE n.Id IS NOT NULL OR r.Id IS NOT NULL
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

			wg.Add(1)
			go func(rid, rname string, writer *io.PipeWriter) {
				defer wg.Done()
				p.writeNzb(db, rid, rname, writer)
			}(id, name, pw)

			category := p.deriveCategory(path)
			select {
			case out <- &ParsedNzb{
				ID:       id,
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

type rarPart struct {
	SegmentIds []string `json:"SegmentIds"`
	PartSize   int64    `json:"PartSize"`
	Offset     int64    `json:"Offset"`
	ByteCount  int64    `json:"ByteCount"`
}

// writeNzb generates the NZB XML and writes it to the pipe
// It uses the shared DB connection pool
func (p *Parser) writeNzb(db *sql.DB, releaseId, releaseName string, pw *io.PipeWriter) {
	defer pw.Close()

	// Fetch files for this release
	rows, err := db.Query(`
		SELECT c.Id, c.Name, c.FileSize, n.SegmentIds, r.RarParts
		FROM DavItems c
		LEFT JOIN DavNzbFiles n ON n.Id = c.Id
		LEFT JOIN DavRarFiles r ON r.Id = c.Id
		WHERE c.ParentId = ? AND (n.Id IS NOT NULL OR r.Id IS NOT NULL)
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
		<meta type="name">` + template.HTMLEscapeString(releaseName) + `</meta>
	</head>
`
	if _, err := pw.Write([]byte(header)); err != nil {
		return
	}

		// Iterate files and write segments
		for rows.Next() {
			var fileId, fileName string
			var fileSize sql.NullInt64
			var segmentIdsJSON sql.NullString
			var rarPartsJSON sql.NullString
	
			if err := rows.Scan(&fileId, &fileName, &fileSize, &segmentIdsJSON, &rarPartsJSON); err != nil {
				slog.Error("Failed to scan file row", "error", err)
				continue
			}
	
			if segmentIdsJSON.Valid {
				var segmentIds []string
				if err := json.Unmarshal([]byte(segmentIdsJSON.String), &segmentIds); err != nil {
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
				subject := template.HTMLEscapeString(fileName)
				if fileId != "" {
					subject = fmt.Sprintf("NZBDAV_ID:%s %s", template.HTMLEscapeString(fileId), template.HTMLEscapeString(fileName))
				}

				fileHeader := fmt.Sprintf(`	<file poster="AltMount" date="%d" subject="%s">
			<groups>
				<group>alt.binaries.test</group>
			</groups>
			<segments>
	`, 0, subject)

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
		} else if rarPartsJSON.Valid {
			var parts []rarPart
			if err := json.Unmarshal([]byte(rarPartsJSON.String), &parts); err != nil {
				slog.Error("Failed to unmarshal RAR parts", "file", fileName, "error", err)
				continue
			}

			for partIdx, part := range parts {
				if len(part.SegmentIds) == 0 {
					continue
				}

				// If it's a RAR archive, we should name the files accordingly so the importer detects it
				// If fileName is "movie.mkv", we name parts "movie.mkv.part01.rar", etc.
				// This ensures the importer processes it as an archive and gets the real file inside.
				partFileName := fmt.Sprintf("%s.part%02d.rar", fileName, partIdx+1)

				totalBytes := part.ByteCount
				bytesPerSegment := int64(0)
				if totalBytes > 0 {
					bytesPerSegment = totalBytes / int64(len(part.SegmentIds))
				}

				// Write File Header
				subject := template.HTMLEscapeString(partFileName)
				if fileId != "" {
					subject = fmt.Sprintf("NZBDAV_ID:%s %s", template.HTMLEscapeString(fileId), template.HTMLEscapeString(partFileName))
				}

				fileHeader := fmt.Sprintf(`	<file poster="AltMount" date="%d" subject="%s">
		<groups>
			<group>alt.binaries.test</group>
		</groups>
		<segments>
`, 0, subject)

				if _, err := pw.Write([]byte(fileHeader)); err != nil {
					return
				}

				// Write Segments
				for i, msgId := range part.SegmentIds {
					segBytes := bytesPerSegment
					// Adjust last segment size
					if i == len(part.SegmentIds)-1 && totalBytes > 0 {
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
		}
	}

	// Write NZB Footer
	if _, err := pw.Write([]byte(`</nzb>`)); err != nil {
		return
	}
}
