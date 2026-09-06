package metadata

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/nzbparser"
)

// allSegmentLists returns every inline SegmentData slice a metadata carries:
// the main file, each PAR2 file, and each nested source. Call
// ExpandSharedOuterSources first so nested sources carry their segments.
func allSegmentLists(m *metapb.FileMetadata) [][]*metapb.SegmentData {
	lists := make([][]*metapb.SegmentData, 0, 1+len(m.Par2Files)+len(m.NestedSources))
	lists = append(lists, m.SegmentData)
	for _, p := range m.Par2Files {
		lists = append(lists, p.SegmentData)
	}
	for _, ns := range m.NestedSources {
		lists = append(lists, ns.Segments)
	}
	return lists
}

// synthesizeStore builds an NzbStore from the inline segments of a legacy
// release group, plus the message-id → flat-index map used to emit SegmentRefs.
//
// Subject/poster/groups are not retained by legacy metas, so entries carry the
// virtual filename as subject and defaultGroup as the single group — enough for
// nzb.BuildNZB to emit a structurally valid NZB. Segment sizes are the decoded
// sizes from SegmentData, which is what segDataToRefs records in decoded_bytes
// and what the read path prefers, so no size information is lost.
//
// One NzbFileEntry per contributing meta keeps each file's segments on a
// contiguous, increasing index range, which is what lets splitRefs collapse
// them into compact SegmentRuns.
func synthesizeStore(files []LegacyMeta, defaultGroup string) (*metapb.NzbStore, map[string]int64, error) {
	store := &metapb.NzbStore{Files: make([]*metapb.NzbFileEntry, 0, len(files))}
	index := make(map[string]int64)
	var flat int64

	var groups []string
	if defaultGroup != "" {
		groups = []string{defaultGroup}
	}

	for _, lm := range files {
		if err := ExpandSharedOuterSources(lm.Meta); err != nil {
			return nil, nil, fmt.Errorf("expand shared outer sources for %s: %w", lm.VirtualPath, err)
		}
		entry := &metapb.NzbFileEntry{
			Subject: filepath.Base(lm.VirtualPath),
			Date:    lm.Meta.CreatedAt,
			Groups:  groups,
		}
		for _, segs := range allSegmentLists(lm.Meta) {
			for _, s := range segs {
				if s.Id == "" {
					return nil, nil, fmt.Errorf("empty segment id in %s", lm.VirtualPath)
				}
				if _, seen := index[s.Id]; seen {
					continue
				}
				index[s.Id] = flat
				flat++
				entry.Segments = append(entry.Segments, &metapb.NzbSeg{
					Id:     s.Id,
					Number: int32(len(entry.Segments) + 1),
					Bytes:  s.SegmentSize,
				})
			}
		}
		store.Files = append(store.Files, entry)
	}
	return store, index, nil
}

// storeHashPath returns the .nzbz path for a group's store. The name embeds the
// first 8 hex characters of the SHA-256 over the store's segment ids in flat
// order, so a store is never overwritten by one holding a different segment set
// — which would silently invalidate the refs of already-migrated metas.
func storeHashPath(dir, groupKey string, store *metapb.NzbStore) string {
	h := sha256.New()
	for _, f := range store.Files {
		for _, s := range f.Segments {
			_, _ = h.Write([]byte(s.Id))
			_, _ = h.Write([]byte{'\n'})
		}
	}
	sum := hex.EncodeToString(h.Sum(nil))[:8]
	return filepath.Join(dir, fmt.Sprintf("%s-%s.nzbz", sanitizeStoreBase(groupKey), sum))
}

// sanitizeStoreBase turns a group key (an nzb path or a directory) into a safe,
// bounded filename component.
func sanitizeStoreBase(groupKey string) string {
	base := filepath.Base(filepath.FromSlash(strings.ReplaceAll(groupKey, `\`, "/")))
	base = strings.TrimSuffix(base, ".nzb")

	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	if out == "" || out == "." || out == ".." {
		out = "release"
	}
	return out
}

// buildGroupStore returns the store and flat index for a release group. It
// prefers a faithful store parsed from the surviving source .nzb (real
// subjects, posters, groups and segment numbers) and falls back to synthesis
// from the inline segments. The bool reports which path was taken.
func buildGroupStore(g LegacyGroup, defaultGroup string) (*metapb.NzbStore, map[string]int64, bool, error) {
	if store, index, ok := tryFaithfulStore(g); ok {
		return store, index, true, nil
	}
	store, index, err := synthesizeStore(g.Files, defaultGroup)
	if err != nil {
		return nil, nil, false, err
	}
	return store, index, false, nil
}

// tryFaithfulStore parses the group's source .nzb, if it still exists, and
// returns the resulting store only when it covers every segment referenced by
// every meta in the group. A partial or mismatched NZB (edited, replaced, or
// belonging to a different release) is rejected outright: half a faithful index
// is worse than an honest synthesized one.
func tryFaithfulStore(g LegacyGroup) (*metapb.NzbStore, map[string]int64, bool) {
	if g.Key == "" {
		return nil, nil, false
	}
	f, err := os.Open(g.Key)
	if err != nil {
		return nil, nil, false
	}
	defer func() { _ = f.Close() }()

	parsed, err := nzbparser.Parse(f)
	if err != nil || parsed == nil || len(parsed.Files) == 0 {
		return nil, nil, false
	}
	store, index := BuildStore(parsed)

	for _, lm := range g.Files {
		if expandErr := ExpandSharedOuterSources(lm.Meta); expandErr != nil {
			return nil, nil, false
		}
		for _, segs := range allSegmentLists(lm.Meta) {
			for _, s := range segs {
				if _, ok := index[s.Id]; !ok {
					return nil, nil, false
				}
			}
		}
	}
	return store, index, true
}
