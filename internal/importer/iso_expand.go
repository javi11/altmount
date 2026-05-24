package importer

import (
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
)

// parsedFileToISOContent adapts a parser.ParsedFile (a bare .iso entry
// in an NZB) to archive.Content so archive.ExpandISOContents can analyse
// it. Mirrors the field mapping rar/processor.go applies to RAR-wrapped
// ISOs, minus RAR-specific InternalPath/PackedSize bookkeeping (bare ISO
// is not packed, so PackedSize == Size).
func parsedFileToISOContent(pf parser.ParsedFile) archive.Content {
	return archive.Content{
		Filename:   pf.Filename,
		Size:       pf.Size,
		PackedSize: pf.Size, // bare ISO is not packed
		NzbdavID:   pf.NzbdavID,
		Segments:   pf.Segments,
		AesKey:     pf.AesKey,
		AesIV:      pf.AesIv, // parser uses AesIv (lowercase v); archive.Content uses AesIV
	}
}

// partitionISOFiles splits a regularFiles slice into the .iso entries
// (case-insensitive) and everything else, preserving original order in
// both outputs.
func partitionISOFiles(files []parser.ParsedFile) (isos, rest []parser.ParsedFile) {
	for _, f := range files {
		if strings.EqualFold(filepath.Ext(f.Filename), ".iso") {
			isos = append(isos, f)
		} else {
			rest = append(rest, f)
		}
	}
	return isos, rest
}
