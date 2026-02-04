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
		// Open in read-only mode to avoid locking issues
		db, err := sql.Open("sqlite3", p.dbPath+"?mode=ro&_journal_mode=WAL")
		if err != nil {
			errChan <- fmt.Errorf("failed to open database: %w", err)
			close(out)
			close(errChan)
			return
		}

		// Ensure DB is closed only after processing is done
		defer func() {
			db.Close()
			close(out)
			close(errChan)
		}()

		// Set limits to prevent file descriptor exhaustion
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)

		// Log available tables for debugging
		tableRows, err := db.Query("SELECT name FROM sqlite_master WHERE type='table'")
		if err == nil {
			var tables []string
			for tableRows.Next() {
				var name string
				tableRows.Scan(&name)
				tables = append(tables, name)
			}
			tableRows.Close()
			slog.Info("NZBDav Database Tables", "tables", tables)
		}

		// Query ALL files, ordered by ParentId
		// This groups files belonging to the same release together efficiently
		rows, err := db.Query(`
			SELECT 
				COALESCE(p.Id, 'root') as ReleaseId,
				COALESCE(p.Name, 'root') as ReleaseName,
				COALESCE(p.Path, '/') as ReleasePath,
				c.Id as FileId,
				c.Name as FileName,
				c.FileSize,
				n.SegmentIds,
				r.RarParts,
				m.Metadata as MultipartMetadata
			FROM DavItems c
			LEFT JOIN DavItems p ON c.ParentId = p.Id
			LEFT JOIN DavNzbFiles n ON n.Id = c.Id
			LEFT JOIN DavRarFiles r ON r.Id = c.Id
			LEFT JOIN DavMultipartFiles m ON m.Id = c.Id
			WHERE (n.Id IS NOT NULL OR r.Id IS NOT NULL OR m.Id IS NOT NULL)
			ORDER BY c.ParentId, c.Name
		`)
		if err != nil {
			errChan <- fmt.Errorf("failed to query files: %w", err)
			return
		}
		defer rows.Close()
		slog.Debug("NZBDav file query completed, starting iteration")

		var currentParentId string
		var currentWriter *io.PipeWriter
		count := 0
		var currentExtractedFiles []ExtractedFileInfo

		// cleanupCurrent ensures the current writer is properly closed
		cleanupCurrent := func() {
			if currentWriter != nil {
				// Write NZB Footer
				if _, err := currentWriter.Write([]byte("</nzb>")); err != nil {
					slog.Error("Failed to write NZB footer", "error", err)
				}
				currentWriter.Close()
				currentWriter = nil
			}
		}
		defer cleanupCurrent()

		for rows.Next() {
			var releaseId, releaseName, releasePath string
			var fileId, fileName string
			var fileSize sql.NullInt64
			var segmentIdsJSON, rarPartsJSON, multipartMetadataJSON sql.RawBytes

			if err := rows.Scan(&releaseId, &releaseName, &releasePath, &fileId, &fileName, &fileSize, &segmentIdsJSON, &rarPartsJSON, &multipartMetadataJSON); err != nil {
				slog.Error("Failed to scan row", "error", err)
				continue
			}

			// Improve release name if it's just "extracted"
			if strings.EqualFold(releaseName, "extracted") {
				// Try to get the name from the path
				pathParts := strings.Split(strings.Trim(releasePath, "/"), "/")
				if len(pathParts) > 0 {
					// Use the last part of the path that isn't "extracted"
					for i := len(pathParts) - 1; i >= 0; i-- {
						if !strings.EqualFold(pathParts[i], "extracted") {
							releaseName = pathParts[i]
							break
						}
					}
				}
			}

			// Check if this file is inside an "extracted" folder
			isExtractedFile := strings.Contains(releasePath, "/extracted") || releaseName == "extracted"
			if isExtractedFile && fileSize.Valid && fileSize.Int64 > 0 {
				currentExtractedFiles = append(currentExtractedFiles, ExtractedFileInfo{
					Name: fileName,
					Size: fileSize.Int64,
				})
			}

			count++
			if count%100 == 0 {
				slog.Info("NZBDav import progress", "files_scanned", count)
			}

			// Check if we switched to a new release
			if releaseId != currentParentId || currentWriter == nil {
				cleanupCurrent()

				currentParentId = releaseId
				currentExtractedFiles = nil // Reset for new release
				slog.Debug("Processing new release", "path", releasePath, "name", releaseName)

				// Create new pipe for this release
				pr, pw := io.Pipe()
				currentWriter = pw

				// Send ParsedNzb to output channel
				category := p.deriveCategory(releasePath)
				relPath := p.deriveRelPath(releasePath, category)

				select {
				case out <- &ParsedNzb{
					ID:             releaseId,
					Name:           releaseName,
					Category:       category,
					RelPath:        relPath,
					Content:        pr,
					ExtractedFiles: currentExtractedFiles,
				}:
				case <-errChan: // Context cancelled or error
					return
				}

				// Write NZB Header
				header := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
	<head>
		<meta type="name">` + template.HTMLEscapeString(releaseName) + `</meta>
	</head>
`
				if _, err := currentWriter.Write([]byte(header)); err != nil {
					slog.Error("Failed to write NZB header", "release", releaseName, "error", err)
					currentWriter.CloseWithError(err)
					currentWriter = nil
					continue
				}
			}

			// Write File Entry
			if err := p.writeFileEntry(currentWriter, fileId, fileName, fileSize, segmentIdsJSON, rarPartsJSON, multipartMetadataJSON); err != nil {
				slog.Error("Failed to write file entry", "file", fileName, "error", err)
				currentWriter.CloseWithError(err)
				currentWriter = nil
			}
		}
		slog.Info("NZBDav import scan completed", "total_files", count)
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

	return ""
}

type rarPart struct {
	SegmentIds []string `json:"SegmentIds"`
	ByteCount  int64    `json:"ByteCount"`
}

type multipartMetadata struct {
	AesParams *aesParams `json:"AesParams"`
	FileParts []filePart `json:"FileParts"`
}

type aesParams struct {
	DecodedSize int64  `json:"DecodedSize"`
	Iv          string `json:"Iv"`
	Key         string `json:"Key"`
}

type filePart struct {
	SegmentIds []string `json:"SegmentIds"`
}

// writeFileEntry writes a single file's segments to the NZB writer
func (p *Parser) writeFileEntry(w io.Writer, fileId, fileName string, fileSize sql.NullInt64, segmentIdsJSON, rarPartsJSON, multipartMetadataJSON sql.RawBytes) error {
	if len(segmentIdsJSON) > 0 {
		var segmentIds []string
		if err := json.Unmarshal(segmentIdsJSON, &segmentIds); err != nil {
			return fmt.Errorf("failed to unmarshal segment IDs: %w", err)
		}

		if len(segmentIds) == 0 {
			return nil
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

		if _, err := w.Write([]byte(fileHeader)); err != nil {
			return err
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

			if _, err := w.Write([]byte(segmentLine)); err != nil {
				return err
			}
		}

		if _, err := w.Write([]byte("		</segments>\n\t</file>\n")); err != nil {
			return err
		}
	} else if len(rarPartsJSON) > 0 {
		var parts []rarPart
		if err := json.Unmarshal(rarPartsJSON, &parts); err != nil {
			return fmt.Errorf("failed to unmarshal RAR parts: %w", err)
		}

		for partIdx, part := range parts {
			if len(part.SegmentIds) == 0 {
				continue
			}

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

			if _, err := w.Write([]byte(fileHeader)); err != nil {
				return err
			}

			// Write Segments
			for i, msgId := range part.SegmentIds {
				segBytes := bytesPerSegment
				if i == len(part.SegmentIds)-1 && totalBytes > 0 {
					segBytes = totalBytes - (bytesPerSegment * int64(i))
				}
				if segBytes <= 0 {
					segBytes = 1
				}

				segmentLine := fmt.Sprintf(`			<segment bytes="%d" number="%d">%s</segment>
`, segBytes, i+1, template.HTMLEscapeString(msgId))

				if _, err := w.Write([]byte(segmentLine)); err != nil {
					return err
				}
			}

			if _, err := w.Write([]byte("  </segments>\n\t</file>\n")); err != nil {

				return err

			}

		}

	} else if len(multipartMetadataJSON) > 0 {

		var meta multipartMetadata

		if err := json.Unmarshal(multipartMetadataJSON, &meta); err != nil {

			return fmt.Errorf("failed to unmarshal multipart metadata: %w", err)

		}

		for partIdx, part := range meta.FileParts {
			if len(part.SegmentIds) == 0 {
				continue
			}

			partFileName := fmt.Sprintf("%s.part%02d", fileName, partIdx+1)
			extraMeta := ""
			if meta.AesParams != nil {
				extraMeta = fmt.Sprintf("AES_KEY:%s AES_IV:%s DECODED_SIZE:%d ",
					meta.AesParams.Key, meta.AesParams.Iv, meta.AesParams.DecodedSize)
			}

			subject := fmt.Sprintf("NZBDAV_ID:%s %s%s",
				template.HTMLEscapeString(fileId), extraMeta, template.HTMLEscapeString(partFileName))

			fileHeader := fmt.Sprintf(`	<file poster="AltMount" date="%d" subject="%s">
		<groups>
			<group>alt.binaries.test</group>
		</groups>
		<segments>
`, 0, subject)

			if _, err := w.Write([]byte(fileHeader)); err != nil {
				return err
			}

			for i, msgId := range part.SegmentIds {
				segmentLine := fmt.Sprintf(`			<segment bytes="%d" number="%d">%s</segment>
`, 750000, i+1, template.HTMLEscapeString(msgId))

				if _, err := w.Write([]byte(segmentLine)); err != nil {
					return err
				}
			}

			if _, err := w.Write([]byte("		</segments>\n\t</file>\n")); err != nil {
				return err
			}
		}
	}
	return nil
}
