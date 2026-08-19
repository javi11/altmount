package par2repair

import (
	"context"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"

	"github.com/javi11/nzbparser"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// ResolveFromNzb builds a repair plan straight from a parsed NZB, for releases
// that were never imported — an archive set dropped at import because its
// volumes had missing articles is exactly that case, and it is where PAR2
// repair is most valuable.
//
// The output is identical in shape to Resolve's, so the planner, solver, job
// and patch store are shared; only the source of the file/segment layout
// differs. Patches are keyed by message ID, so a patch written here is the
// same one the streaming read path uses after the release imports.
func ResolveFromNzb(
	ctx context.Context,
	n *nzbparser.Nzb,
	deadSegmentIDs []string,
	fetch ArticleFetcher,
	caps Caps,
) (*Resolution, error) {
	if n == nil || len(n.Files) == 0 {
		return nil, fmt.Errorf("%w: empty NZB", ErrUnrepairable)
	}

	// Split the NZB into PAR2 files and candidate content files.
	var par2Entries, contentEntries []nzbparser.NzbFile
	for _, f := range n.Files {
		if isPar2Filename(nzbFileName(f)) {
			par2Entries = append(par2Entries, f)
		} else {
			contentEntries = append(contentEntries, f)
		}
	}
	if len(par2Entries) == 0 {
		return nil, fmt.Errorf("%w: NZB carries no PAR2 files", ErrUnrepairable)
	}
	if len(contentEntries) == 0 {
		return nil, fmt.Errorf("%w: NZB carries no content files", ErrUnrepairable)
	}

	// Smallest PAR2 file first so the index (which carries Main/FileDesc/IFSC)
	// is parsed before the recovery volumes.
	sort.Slice(par2Entries, func(i, j int) bool {
		return nzbFileBytes(par2Entries[i]) < nzbFileBytes(par2Entries[j])
	})

	cache := newArticleCache(resolveCacheCap)
	var par2Files []SetFile
	var streams []io.Reader
	for _, f := range par2Entries {
		sf := nzbEntryToSetFile(f, nil)
		par2Files = append(par2Files, sf)
		streams = append(streams, newLazyFileReader(ctx, fetch, sf, cache))
	}

	// Match recovery-set members to NZB content entries, reusing the shared
	// matcher by presenting the entries as NzbStore rows.
	store := &metapb.NzbStore{}
	for _, f := range contentEntries {
		entry := &metapb.NzbFileEntry{Subject: nzbSubjectFor(f)}
		for _, s := range f.Segments {
			entry.Segments = append(entry.Segments, &metapb.NzbSeg{
				Id:     s.ID,
				Number: int32(s.Number),
				Bytes:  int64(s.Bytes),
			})
		}
		store.Files = append(store.Files, entry)
	}

	dead := map[string]bool{}
	for _, id := range deadSegmentIDs {
		if id != "" {
			dead[normalizeMsgID(id)] = true
		}
	}
	if err := statSweep(ctx, fetch, releaseArticleIDs(store, par2Files), dead); err != nil {
		return nil, err
	}
	// store carries only content entries here, so nothing to exclude.
	if err := ratioPrecheck(store.Files, nil, dead, caps); err != nil {
		return nil, err
	}

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		return nil, fmt.Errorf("%w: parse PAR2 set: %v", ErrUnrepairable, err)
	}
	dropDeadRecovery(idx, par2Files, dead)

	files, err := matchSetFiles(ctx, idx, store, dead, fetch, cache)
	if err != nil {
		return nil, err
	}

	plan, err := BuildPlan(idx, files, caps)
	if err != nil {
		return nil, err
	}
	return &Resolution{Plan: plan, Index: idx, Par2Files: par2Files}, nil
}

// nzbEntryToSetFile converts one NZB file entry into a SetFile. dead may be nil.
func nzbEntryToSetFile(f nzbparser.NzbFile, dead map[string]bool) SetFile {
	sf := SetFile{}
	var total int64
	for _, s := range f.Segments {
		id := normalizeMsgID(s.ID)
		sf.Articles = append(sf.Articles, Article{
			MessageID: id,
			Size:      int64(s.Bytes),
			Dead:      dead[id],
		})
		total += int64(s.Bytes)
	}
	sf.Length = uint64(total)
	return sf
}

// nzbFileName returns the entry's filename, falling back to the subject when
// the parser could not extract one (fully obfuscated posts).
func nzbFileName(f nzbparser.NzbFile) string {
	if f.Filename != "" {
		return f.Filename
	}
	return subjectFilename(f.Subject)
}

// nzbSubjectFor returns a subject that carries the filename, so the shared
// matcher's filename pass can work even when the original subject is
// obfuscated but the parser recovered a name.
func nzbSubjectFor(f nzbparser.NzbFile) string {
	if f.Filename != "" && !strings.Contains(f.Subject, f.Filename) {
		return f.Subject + " " + f.Filename
	}
	return f.Subject
}

func nzbFileBytes(f nzbparser.NzbFile) int64 {
	if f.Bytes > 0 {
		return f.Bytes
	}
	var total int64
	for _, s := range f.Segments {
		total += int64(s.Bytes)
	}
	return total
}

// par2FilenamePattern matches .par2 and .volNNN+NNN.par2 names.
var par2FilenamePattern = regexp.MustCompile(`(?i)\.par2$`)

// isPar2Filename reports whether a filename names a PAR2 file.
func isPar2Filename(name string) bool {
	return par2FilenamePattern.MatchString(strings.TrimSpace(name))
}

// subjectQuotedName extracts a "quoted filename" from a usenet subject.
var subjectQuotedName = regexp.MustCompile(`"([^"]+)"`)

// subjectFilename returns the quoted filename in a subject, or the raw subject
// when it carries no quoted name.
func subjectFilename(subject string) string {
	if m := subjectQuotedName.FindStringSubmatch(subject); len(m) == 2 {
		return m[1]
	}
	return subject
}
