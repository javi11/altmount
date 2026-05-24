package importer

import (
	"context"
	"fmt"
	"log/slog"
	"path"
	"path/filepath"
	"strings"

	"github.com/javi11/altmount/internal/importer/archive"
	"github.com/javi11/altmount/internal/importer/parser"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
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

// expandBareISODeps lets the orchestrator be tested without an NNTP pool
// or a real metadata service. Production wiring constructs these from
// the Processor's existing collaborators.
type expandBareISODeps struct {
	expand        func(ctx context.Context, enabled bool, contents []archive.Content) ([]archive.Content, error)
	writeMetadata func(virtualPath string, meta *metapb.FileMetadata) error
	// enabled is the resolved value of Import.ExpandBlurayIso. Pulled
	// out of deps so tests can flip it without touching config.
	enabled bool
}

// expandBareISOFiles peels .iso entries out of regularFiles, runs the
// existing archive.ExpandISOContents over them (which handles single-disc
// playlist resolution AND multi-disc DISC_N grouping), writes each
// expanded Content as a FileMetadata under virtualDir, and returns the
// list of written virtual paths plus the remaining (non-ISO + unchanged)
// files for normal dispatch.
//
// When no .iso files are present, returns (nil, regularFiles, nil).
// When deps.enabled is false, archive.ExpandISOContents returns the
// inputs unchanged; in that case we push the ISOs back into `remaining`
// so processSingleFile/processMultiFile handle them as raw .iso bytes.
//
// Pairing-by-position note: archive.ExpandISOContents appends exactly one
// Content per input ISO when no multi-disc merging happens, so the i-th
// expanded output corresponds to isos[i]. When multi-disc merging DOES
// happen (group of N discs collapses into 1 Content), every entry in the
// returned slice has NestedSources populated — the per-index fallback
// branch (which references isos[i]) is therefore never taken in that case.
func expandBareISOFiles(
	ctx context.Context,
	deps expandBareISODeps,
	regularFiles []parser.ParsedFile,
	virtualDir string,
	releaseName string,
) (written []string, remaining []parser.ParsedFile, err error) {
	isos, rest := partitionISOFiles(regularFiles)
	if len(isos) == 0 {
		return nil, regularFiles, nil
	}

	in := make([]archive.Content, 0, len(isos))
	for _, pf := range isos {
		in = append(in, parsedFileToISOContent(pf))
	}

	expanded, err := deps.expand(ctx, deps.enabled, in)
	if err != nil {
		return nil, nil, fmt.Errorf("expand bare ISOs: %w", err)
	}

	for i, c := range expanded {
		if c.ISOExpansionIndex == 0 && len(c.NestedSources) == 0 {
			// Untransformed — fall back to standard processing.
			remaining = append(remaining, isos[i])
			continue
		}
		// Task 4 wiring will supply real sourceNzbPath/releaseDate values;
		// for now plumb empty strings/zero — see archive.NewFileMetadataFromContent
		// signature.
		meta := archive.NewFileMetadataFromContent(c, "", 0, c.NzbdavID)
		virtualPath := path.Join(virtualDir, c.Filename)
		if err := deps.writeMetadata(virtualPath, meta); err != nil {
			return written, nil, fmt.Errorf("write metadata %q: %w", virtualPath, err)
		}
		written = append(written, virtualPath)
		slog.InfoContext(ctx, "Expanded bare ISO into virtual file",
			"release", releaseName,
			"path", virtualPath,
			"size", c.Size,
			"nested_sources", len(c.NestedSources),
		)
	}
	remaining = append(remaining, rest...)
	return written, remaining, nil
}
