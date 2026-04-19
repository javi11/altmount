package nzbdav

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/klauspost/compress/zstd"
	_ "github.com/mattn/go-sqlite3"
)

// zstd frame magic: 0x28 0xB5 0x2F 0xFD.
// nzbdav's blobstore stores some NZBs compressed and some as plain XML, so
// sniff the header before deciding whether to decompress.
var zstdMagic = []byte{0x28, 0xB5, 0x2F, 0xFD}

const (
	nzbPoster       = "nzbdav"
	nzbGroup        = "alt.binaries.misc"
	defaultSegBytes = 750_000
)

type Parser struct {
	dbPath    string
	blobsPath string
}

func NewParser(dbPath, blobsPath string) *Parser {
	return &Parser{
		dbPath:    dbPath,
		blobsPath: blobsPath,
	}
}

// davItem mirrors the DavItems row subset we need to resolve releases
// and discover extracted-files subtrees.
type davItem struct {
	ID       string
	ParentID sql.NullString
	Name     string
	Path     string
	FileSize sql.NullInt64
}

// davTree indexes DavItems by ID and by parent ID for O(1) lookups.
type davTree struct {
	byID       map[string]*davItem
	byParentID map[string][]*davItem
}

// Parse streams NZBs from the database
func (p *Parser) Parse() (<-chan *ParsedNzb, <-chan error) {
	out := make(chan *ParsedNzb)
	errChan := make(chan error, 1)

	go func() {
		db, err := sql.Open("sqlite3", p.dbPath+"?mode=ro&_journal_mode=WAL")
		if err != nil {
			errChan <- fmt.Errorf("failed to open database: %w", err)
			close(out)
			close(errChan)
			return
		}

		defer func() {
			db.Close()
			close(out)
			close(errChan)
		}()

		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)

		tree, err := loadDavTree(db)
		if err != nil {
			errChan <- fmt.Errorf("failed to load DavItems tree: %w", err)
			return
		}

		// Blob-based (alpha) storage is detected by presence of the NzbNames table
		// and a configured blobs directory.
		if p.blobsPath != "" && hasTable(db, "NzbNames") {
			slog.InfoContext(context.Background(), "Detected blob-based NZBDav storage")
			p.parseBlobs(db, tree, out, errChan)
			return
		}

		p.parseLegacy(db, tree, out, errChan)
	}()

	return out, errChan
}

func hasTable(db *sql.DB, name string) bool {
	var count int
	if err := db.QueryRow(
		"SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?", name,
	).Scan(&count); err != nil {
		return false
	}
	return count > 0
}

func loadDavTree(db *sql.DB) (*davTree, error) {
	rows, err := db.Query(`SELECT Id, ParentId, Name, COALESCE(Path, ''), FileSize FROM DavItems`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tree := &davTree{
		byID:       make(map[string]*davItem),
		byParentID: make(map[string][]*davItem),
	}
	for rows.Next() {
		it := &davItem{}
		if err := rows.Scan(&it.ID, &it.ParentID, &it.Name, &it.Path, &it.FileSize); err != nil {
			return nil, err
		}
		tree.byID[it.ID] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, it := range tree.byID {
		if it.ParentID.Valid {
			tree.byParentID[it.ParentID.String] = append(tree.byParentID[it.ParentID.String], it)
		}
	}
	return tree, nil
}

// releaseFor returns the nzbdav folder that logically groups this file — the
// nearest ancestor folder whose name is not "extracted". For a file at
// /content/uncategorized/My.Release/file.mkv the release is My.Release; for
// /movies/Release/extracted/file.mkv it's still Release (the /extracted folder
// is skipped).
func (t *davTree) releaseFor(id string) *davItem {
	it, ok := t.byID[id]
	if !ok {
		return nil
	}
	cur := it
	if cur.ParentID.Valid {
		cur = t.byID[cur.ParentID.String]
	} else {
		return it
	}
	for cur != nil {
		if !strings.EqualFold(cur.Name, "extracted") {
			return cur
		}
		if !cur.ParentID.Valid {
			return cur
		}
		cur = t.byID[cur.ParentID.String]
	}
	return it
}

// extractedFilesUnder returns all descendant items whose path contains
// "/extracted/" and that have a positive FileSize.
func (t *davTree) extractedFilesUnder(releaseID string) []ExtractedFileInfo {
	var out []ExtractedFileInfo
	var walk func(id string)
	walk = func(id string) {
		for _, child := range t.byParentID[id] {
			if strings.Contains(child.Path, "/extracted/") && child.FileSize.Valid && child.FileSize.Int64 > 0 {
				out = append(out, ExtractedFileInfo{Name: child.Name, Size: child.FileSize.Int64})
			}
			walk(child.ID)
		}
	}
	walk(releaseID)
	return out
}

func (p *Parser) parseBlobs(db *sql.DB, tree *davTree, out chan<- *ParsedNzb, errChan chan<- error) {
	rows, err := db.Query(`
		SELECT
			d.Id,
			n.FileName,
			COALESCE(d.Path, '/') as ReleasePath,
			d.NzbBlobId
		FROM DavItems d
		JOIN NzbNames n ON n.Id = d.NzbBlobId
		WHERE d.NzbBlobId IS NOT NULL
		AND d.NzbBlobId != ''
		AND d.NzbBlobId != '00000000-0000-0000-0000-000000000000'
		AND d.SubType = 203
	`)
	if err != nil {
		errChan <- fmt.Errorf("failed to query blob files: %w", err)
		return
	}
	defer rows.Close()

	count := 0
	for rows.Next() {
		var id, fileName, releasePath, blobId string
		if err := rows.Scan(&id, &fileName, &releasePath, &blobId); err != nil {
			slog.ErrorContext(context.Background(), "Failed to scan blob row", "error", err)
			continue
		}

		if len(blobId) < 4 {
			slog.WarnContext(context.Background(), "Invalid blob ID", "id", blobId)
			continue
		}
		blobPath := filepath.Join(p.blobsPath, blobId[0:2], blobId[2:4], blobId)

		blobFile, err := os.Open(blobPath)
		if err != nil {
			slog.ErrorContext(context.Background(), "Failed to open blob file", "path", blobPath, "error", err)
			continue
		}

		pr, pw := io.Pipe()
		go func() {
			defer blobFile.Close()

			br := bufio.NewReader(blobFile)
			head, _ := br.Peek(len(zstdMagic))
			if bytes.Equal(head, zstdMagic) {
				zr, err := zstd.NewReader(br)
				if err != nil {
					pw.CloseWithError(err)
					return
				}
				defer zr.Close()
				if _, err := io.Copy(pw, zr); err != nil {
					pw.CloseWithError(err)
					return
				}
			} else {
				if _, err := io.Copy(pw, br); err != nil {
					pw.CloseWithError(err)
					return
				}
			}
			pw.Close()
		}()

		// The SubType=203 item is the virtual "output" file. Its parent folder
		// is the release directory in nzbdav's tree; we want AltMount's mount
		// layout to match that directory layout exactly.
		release := tree.releaseFor(id)
		releaseName := strings.TrimSuffix(fileName, ".nzb")
		releaseParentPath := releasePath
		releaseID := id
		if release != nil {
			releaseID = release.ID
			if release.Name != "" {
				releaseName = release.Name
			}
			releaseParentPath = release.Path
		}
		// Strip the release folder name to get its parent path.
		parentPath := trimLastSegment(releaseParentPath)
		category, relPath := p.splitPath(parentPath)

		out <- &ParsedNzb{
			ID:             id,
			Name:           releaseName,
			Category:       category,
			RelPath:        relPath,
			Content:        pr,
			ExtractedFiles: tree.extractedFilesUnder(releaseID),
		}
		count++
	}
	slog.InfoContext(context.Background(), "NZBDav blob import scan completed", "total_files", count)
}

func (p *Parser) parseLegacy(db *sql.DB, tree *davTree, out chan<- *ParsedNzb, errChan chan<- error) {
	rows, err := db.Query(`
		SELECT c.Id, c.Name, c.FileSize, n.SegmentIds
		FROM DavItems c
		JOIN DavNzbFiles n ON n.Id = c.Id
	`)
	if err != nil {
		errChan <- fmt.Errorf("failed to query files: %w", err)
		return
	}
	defer rows.Close()

	// Group file rows by the resolved release id.
	grouped := make(map[string][]nzbFileRow)
	releaseOrder := make([]string, 0)
	count := 0
	for rows.Next() {
		var r nzbFileRow
		if err := rows.Scan(&r.fileID, &r.fileName, &r.fileSize, &r.segmentIDs); err != nil {
			slog.ErrorContext(context.Background(), "Failed to scan row", "error", err)
			continue
		}

		release := tree.releaseFor(r.fileID)
		if release == nil {
			continue
		}

		// Clone RawBytes: driver reuses the underlying buffer on next Scan.
		segCopy := make(sql.RawBytes, len(r.segmentIDs))
		copy(segCopy, r.segmentIDs)
		r.segmentIDs = segCopy

		if _, seen := grouped[release.ID]; !seen {
			releaseOrder = append(releaseOrder, release.ID)
		}
		grouped[release.ID] = append(grouped[release.ID], r)
		count++
	}
	if err := rows.Err(); err != nil {
		errChan <- fmt.Errorf("failed to iterate files: %w", err)
		return
	}

	slog.InfoContext(context.Background(), "NZBDav import scan completed", "total_files", count, "releases", len(releaseOrder))

	for _, releaseID := range releaseOrder {
		release := tree.byID[releaseID]
		if release == nil {
			continue
		}

		// Skip files that are themselves inside an /extracted subtree — they're
		// recorded separately in ExtractedFiles rather than emitted as NZB entries.
		var primary []nzbFileRow
		for _, r := range grouped[releaseID] {
			item := tree.byID[r.fileID]
			if item != nil && strings.Contains(item.Path, "/extracted/") {
				continue
			}
			primary = append(primary, r)
		}
		if len(primary) == 0 {
			continue
		}

		category, relPath := p.splitPath(trimLastSegment(release.Path))
		releaseName := release.Name
		if strings.EqualFold(releaseName, "extracted") {
			// Defensive: releaseFor should have stopped one level higher, but
			// preserve the original fallback just in case the tree is malformed.
			parts := strings.Split(strings.Trim(release.Path, "/"), "/")
			for i := len(parts) - 1; i >= 0; i-- {
				if !strings.EqualFold(parts[i], "extracted") {
					releaseName = parts[i]
					break
				}
			}
		}

		pr, pw := io.Pipe()
		parsed := &ParsedNzb{
			ID:             releaseID,
			Name:           releaseName,
			Category:       category,
			RelPath:        relPath,
			Content:        pr,
			ExtractedFiles: tree.extractedFilesUnder(releaseID),
		}

		out <- parsed

		go writeReleaseNzb(pw, releaseName, primary)
	}
}

func writeReleaseNzb(pw *io.PipeWriter, releaseName string, files []nzbFileRow) {
	header := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE nzb PUBLIC "-//newzBin//DTD NZB 1.1//EN" "http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
<nzb xmlns="http://www.newzbin.com/DTD/nzb/nzb-1.1.dtd">
	<head>
		<meta type="name">` + template.HTMLEscapeString(releaseName) + `</meta>
	</head>
`
	if _, err := pw.Write([]byte(header)); err != nil {
		pw.CloseWithError(err)
		return
	}

	warnedMissingSize := false
	for _, f := range files {
		if !f.fileSize.Valid && !warnedMissingSize {
			slog.WarnContext(context.Background(),
				"NZBDav file has no FileSize; using default segment size",
				"release", releaseName, "file", f.fileName, "default_bytes", defaultSegBytes)
			warnedMissingSize = true
		}
		if err := writeFileEntry(pw, f.fileID, f.fileName, f.fileSize, f.segmentIDs); err != nil {
			slog.ErrorContext(context.Background(), "Failed to write file entry",
				"release", releaseName, "file", f.fileName, "error", err)
			pw.CloseWithError(err)
			return
		}
	}

	if _, err := pw.Write([]byte("</nzb>")); err != nil {
		pw.CloseWithError(err)
		return
	}
	pw.Close()
}

type nzbFileRow struct {
	fileID     string
	fileName   string
	fileSize   sql.NullInt64
	segmentIDs sql.RawBytes
}

// splitPath splits a path into (firstSegment, rest) so that
// filepath.Join(firstSegment, rest, releaseName) reproduces the original
// nzbdav path verbatim. This preserves nzbdav's folder structure — no
// movies/tv/other bucketing.
//
// Example: "/content/uncategorized" → ("content", "uncategorized").
// Example: "/movies"                → ("movies", "").
// Example: "" or "/"                → ("other", "") as a safe default.
func (p *Parser) splitPath(path string) (first, rest string) {
	cleaned := strings.ReplaceAll(path, "\\", "/")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return "other", ""
	}
	parts := strings.Split(cleaned, "/")
	return parts[0], strings.Join(parts[1:], "/")
}

// trimLastSegment drops the final path segment (the release or file name),
// returning the parent path. "/a/b/c" → "/a/b", "/a" → "", "/" → "".
func trimLastSegment(path string) string {
	cleaned := strings.ReplaceAll(path, "\\", "/")
	cleaned = strings.Trim(cleaned, "/")
	if cleaned == "" {
		return ""
	}
	parts := strings.Split(cleaned, "/")
	if len(parts) <= 1 {
		return ""
	}
	return "/" + strings.Join(parts[:len(parts)-1], "/")
}

// writeFileEntry writes a single file's segments to the NZB writer.
func writeFileEntry(w io.Writer, fileId, fileName string, fileSize sql.NullInt64, segmentIdsJSON sql.RawBytes) error {
	if len(segmentIdsJSON) == 0 {
		return nil
	}

	var segmentIds []string
	if err := json.Unmarshal(segmentIdsJSON, &segmentIds); err != nil {
		return fmt.Errorf("failed to unmarshal segment IDs: %w", err)
	}
	if len(segmentIds) == 0 {
		return nil
	}

	totalBytes := int64(0)
	if fileSize.Valid {
		totalBytes = fileSize.Int64
	}

	bytesPerSegment := int64(defaultSegBytes)
	if totalBytes > 0 {
		bytesPerSegment = totalBytes / int64(len(segmentIds))
		if bytesPerSegment <= 0 {
			bytesPerSegment = 1
		}
	}

	subject := template.HTMLEscapeString(fileName)
	if fileId != "" {
		subject = fmt.Sprintf("NZBDAV_ID:%s %s", template.HTMLEscapeString(fileId), template.HTMLEscapeString(fileName))
	}

	fileHeader := fmt.Sprintf(`	<file poster="%s" date="%d" subject="%s">
		<groups>
			<group>%s</group>
		</groups>
		<segments>
`, nzbPoster, 0, subject, nzbGroup)

	if _, err := w.Write([]byte(fileHeader)); err != nil {
		return err
	}

	for i, msgId := range segmentIds {
		segBytes := bytesPerSegment
		if i == len(segmentIds)-1 && totalBytes > 0 {
			segBytes = totalBytes - (bytesPerSegment * int64(i))
			if segBytes <= 0 {
				segBytes = bytesPerSegment
			}
		}
		if segBytes <= 0 {
			segBytes = defaultSegBytes
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
	return nil
}
