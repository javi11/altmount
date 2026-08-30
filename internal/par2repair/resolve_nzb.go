package par2repair

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/javi11/nntppool/v4"
	"github.com/javi11/nzbparser"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/nzbgap"
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
	log *slog.Logger,
	progress JobProgress,
) (*Resolution, error) {
	if n == nil || len(n.Files) == 0 {
		return nil, fmt.Errorf("%w: empty NZB", ErrUnrepairable)
	}

	// Segment numbers the NZB never listed become synthetic placeholder
	// articles, pre-marked dead: no provider can serve an ID we minted, and
	// their byte positions are exactly what the plan needs to treat the gap's
	// slices as missing. The patch is later stored under the same
	// deterministic ID the importer inserts, which is where the read path's
	// patch-before-fetch lookup finds it.
	gaps := nzbgap.Fill(n)
	var gapIDs []string
	for _, ids := range gaps {
		gapIDs = append(gapIDs, ids...)
	}
	if len(gapIDs) > 0 {
		log.InfoContext(ctx, "NZB omits segment numbers; planning synthetic placeholder articles as dead",
			"files_with_gaps", len(gaps), "missing_segments", len(gapIDs))
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
	for _, f := range par2Entries {
		par2Files = append(par2Files, nzbEntryToSetFile(f, nil))
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
	for _, id := range gapIDs {
		dead[normalizeMsgID(id)] = true
	}
	// Articles already repaired locally are alive: the fetcher serves their
	// patch. Without this a completed repair re-plans (and re-downloads the
	// release) on every cycle, since providers still 430 the originals.
	if pc, ok := fetch.(PatchChecker); ok {
		for id := range dead {
			if pc.HasPatch(id) {
				delete(dead, id)
			}
		}
	}
	if err := releaseSizePrecheck(store.Files, par2Files, caps); err != nil {
		return nil, err
	}

	started := time.Now()
	hidden, err := statSweep(ctx, fetch, releaseArticleIDs(store, par2Files), dead, progress)
	if err != nil {
		return nil, err
	}
	caps.ExpectedHiddenArticles = hidden
	log.InfoContext(ctx, "PAR2 repair liveness check complete",
		"dead_articles", len(dead), "hidden_estimate", hidden, "duration", time.Since(started).Round(time.Millisecond))
	// Flip the reported stage off "checking" now: the sizing probes below are
	// minutes of article fetches, and until planSet reports again the UI would
	// otherwise sit on a finished liveness count.
	if progress != nil {
		progress(StagePlanning, 0, 0)
	}
	// store carries only content entries here, so nothing to exclude.
	if err := ratioPrecheck(store.Files, nil, dead, caps); err != nil {
		return nil, err
	}

	// The NZB declares yEnc-ENCODED segment sizes — a few percent above the
	// decoded payloads. Fine for thresholds, fatal for the byte-exact stream
	// offsets the PAR2 parser and the recovery payload reads rely on: every
	// article boundary would drift and every recovery payload would fail its
	// packet MD5. Content members are sized during matching (sizeArticles);
	// the PAR2 files themselves are sized here.
	if err := sizePar2SetFiles(ctx, fetch, par2Files, dead, cache, log); err != nil {
		return nil, err
	}

	idx, files, err := planSet(ctx, fetch, store, par2Files, dead, cache, log, progress)
	if err != nil {
		return nil, err
	}

	// Dead articles in files the recovery set does not cover (unmatched
	// entries, or gaps in unprotected sidecars) can never be repaired here.
	// With repairable damage the job still runs — partial repair is worth it —
	// but once nothing plannable remains, surface the coverage hole as
	// permanent: the alternative is an endless defer → repair → defer loop,
	// each cycle streaming the whole release again.
	if uncovered := uncoveredDeadArticles(dead, files, par2Files); len(uncovered) > 0 {
		if !anyMemberArticleDead(dead, files) {
			return nil, fmt.Errorf("%w: %d missing article(s) (e.g. %s) are not covered by the PAR2 recovery set",
				ErrUnrepairable, len(uncovered), uncovered[0])
		}
		log.WarnContext(ctx, "Some missing articles are outside the PAR2 recovery set and cannot be repaired",
			"uncovered", len(uncovered), "example", uncovered[0])
	}

	plan, err := BuildPlan(idx, files, caps)
	if err != nil {
		return nil, err
	}
	log.InfoContext(ctx, "PAR2 repair plan built",
		"missing_slices", len(plan.Missing), "recovery_rows", len(plan.Recovery),
		"spares", len(plan.SpareRecovery), "spill_to_disk", plan.SpillToDisk)
	return &Resolution{Plan: plan, Index: idx, Par2Files: par2Files}, nil
}

// uncoveredDeadArticles lists dead article IDs that belong to neither a
// recovery-set member nor a PAR2 file — damage the recovery set cannot see,
// let alone repair. (A dead PAR2 article costs spares, not repairability.)
func uncoveredDeadArticles(dead map[string]bool, members []SetFile, par2Files []SetFile) []string {
	covered := map[string]bool{}
	for _, f := range members {
		for _, a := range f.Articles {
			covered[a.MessageID] = true
		}
	}
	for _, f := range par2Files {
		for _, a := range f.Articles {
			covered[a.MessageID] = true
		}
	}
	var out []string
	for id := range dead {
		if !covered[id] {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// anyMemberArticleDead reports whether any recovery-set member has a dead
// article — i.e. whether the plan will have missing slices worth repairing.
func anyMemberArticleDead(dead map[string]bool, members []SetFile) bool {
	for _, f := range members {
		for _, a := range f.Articles {
			if dead[a.MessageID] {
				return true
			}
		}
	}
	return false
}

// maxSizeProbesPerFile bounds how many articles one file's size derivation
// may probe before deferring to the release-wide part size. Usenet posts use
// one uniform decoded part size, so any live article answers the question;
// probing beyond a few candidates only multiplies wire time on releases whose
// articles keep failing (each failed probe costs seconds to minutes on a
// flapping provider).
const maxSizeProbesPerFile = 4

// sizePar2SetFiles replaces the PAR2 files' declared (encoded) article sizes
// with decoded ones, probed from the articles themselves: usenet posts split
// each file at one uniform decoded part size, so one live non-final article
// gives every middle article's size and the final article is probed directly.
// The probed payloads land in the shared cache, so the parse that follows
// reuses them instead of refetching.
//
// Dead articles cannot be probed. A volume with no probeable non-final article
// borrows the release-wide decoded part size instead, in a second pass: usenet
// posts split every file of a release at one uniform decoded part size, so a
// sibling's probe is authoritative. Keeping such a volume on its yEnc-ENCODED
// NZB sizes would drift its length and every BodyOffset inside it by the yEnc
// overhead, making each of its recovery payloads fail its RecvSlic packet MD5
// and be dropped as unreachable — silently costing the repair a whole volume's
// recovery rows.
//
// A volume whose FINAL article is also dead keeps that article's declared size:
// unlike a content member, a PAR2 volume has no FileDesc recording its true
// length, so the remainder is unknowable. Every offset before it is still
// corrected, which is what locating recovery payloads depends on.
func sizePar2SetFiles(
	ctx context.Context,
	fetch ArticleFetcher,
	files []SetFile,
	dead map[string]bool,
	cache *articleCache,
	log *slog.Logger,
) error {
	probe := func(id string) (int64, error) {
		payload, err := fetchCached(ctx, fetch, id, cache)
		if err != nil {
			if errors.Is(err, nntppool.ErrArticleNotFound) {
				dead[id] = true
				return -1, nil
			}
			return -1, fmt.Errorf("par2repair: probe par2 article %s: %w", id, err)
		}
		return int64(len(payload)), nil
	}

	// Warm every probe target up front: the sizing loop below is sequential
	// and would otherwise pay one fetch round-trip per file per probe.
	for i := range files {
		if n := len(files[i].Articles); n > 0 {
			for _, a := range []Article{files[i].Articles[0], files[i].Articles[n-1]} {
				if !dead[a.MessageID] {
					cache.warm(ctx, fetch, a.MessageID)
				}
			}
		}
	}

	// apply writes decoded sizes for one volume from a known part size, and
	// the probed final-article size when there is one.
	apply := func(f *SetFile, partSize, lastSize int64, source SizeProvenance) {
		n := len(f.Articles)
		f.SizeSource = source
		var total int64
		for j := range f.Articles {
			switch {
			case j < n-1:
				f.Articles[j].Size = partSize
			case lastSize >= 0:
				f.Articles[j].Size = lastSize
				// else: dead final article keeps its declared size; a PAR2
				// volume has no FileDesc length to derive the remainder from.
			}
			total += f.Articles[j].Size
		}
		f.Length = uint64(total)
	}

	// Pass 1: size every volume that has a probeable non-final article, and
	// remember the release-wide decoded part size.
	releasePartSize := int64(-1)
	var deferred []int
	for i := range files {
		f := &files[i]
		n := len(f.Articles)
		if n == 0 {
			continue
		}

		// Final article first: its decoded size is not uniform.
		lastSize := int64(-1)
		if last := f.Articles[n-1]; !dead[last.MessageID] {
			var err error
			if lastSize, err = probe(last.MessageID); err != nil {
				return err
			}
		}

		partSize := int64(-1)
		// Bounded walk: with a flapping provider every failed probe costs tens
		// of seconds, so an article-by-article march through a large dead
		// volume would stall planning for hours. Any live article yields the
		// same uniform part size, so after the cap the volume defers to the
		// release-wide size (pass 2) instead.
		probes := 0
		for j := 0; j < n-1 && partSize < 0 && probes < maxSizeProbesPerFile; j++ {
			if a := f.Articles[j]; !dead[a.MessageID] {
				probes++
				var err error
				if partSize, err = probe(a.MessageID); err != nil {
					return err
				}
			}
		}

		if n > 1 && partSize < 0 {
			deferred = append(deferred, i)
			continue
		}
		if partSize > 0 && releasePartSize < 0 {
			releasePartSize = partSize
		}
		apply(f, partSize, lastSize, SizeProbed)
	}

	// Pass 2: volumes with nothing probeable borrow the release's part size.
	for _, i := range deferred {
		f := &files[i]
		if releasePartSize < 0 {
			log.WarnContext(ctx, "no PAR2 volume in the release had a probeable non-final article; keeping encoded sizes",
				"first_article", f.Articles[0].MessageID)
			f.SizeSource = SizeEncodedFallback
			continue
		}
		lastSize := int64(-1)
		if last := f.Articles[len(f.Articles)-1]; !dead[last.MessageID] {
			var err error
			if lastSize, err = probe(last.MessageID); err != nil {
				return err
			}
		}
		log.WarnContext(ctx, "PAR2 volume's non-final articles are all dead; borrowing the release part size",
			"first_article", f.Articles[0].MessageID, "part_size", releasePartSize)
		apply(f, releasePartSize, lastSize, SizeBorrowedHint)
	}
	return nil
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
