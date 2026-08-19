package par2repair

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/javi11/nntppool/v4"

	"github.com/javi11/altmount/internal/importer/parser/par2"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
)

// Resolution is everything RunJob needs, derived from a file's metadata.
type Resolution struct {
	Plan      *Plan
	Index     *par2.Index
	Par2Files []SetFile
}

// Resolve turns a damaged file's metadata into a repair plan:
//
//  1. Parse the PAR2 set from the metadata's Par2Files segments (recovery
//     payloads are located, not downloaded, via seek-aware lazy readers).
//  2. Match every recovery-set member (RAR volume / content file) to its
//     NzbStore entry — by filename in the subject first, then by Hash16k of
//     the file's first bytes.
//  3. Derive per-article decoded sizes from the first live article (usenet
//     posts use a uniform part size; the last article takes the remainder).
//  4. Mark dead articles (trigger's failing segment + persisted known holes)
//     and build the plan under the given caps.
func Resolve(
	ctx context.Context,
	fm *metapb.FileMetadata,
	store *metapb.NzbStore,
	deadSegmentIDs []string,
	fetch ArticleFetcher,
	caps Caps,
) (*Resolution, error) {
	if len(fm.Par2Files) == 0 {
		return nil, fmt.Errorf("%w: no PAR2 files recorded for this release", ErrUnrepairable)
	}
	if store == nil || len(store.Files) == 0 {
		return nil, fmt.Errorf("%w: no NzbStore for this release", ErrUnrepairable)
	}

	// 1. PAR2 set files, smallest first so the index file is parsed cheaply.
	par2Refs := make([]*metapb.Par2FileReference, len(fm.Par2Files))
	copy(par2Refs, fm.Par2Files)
	sort.Slice(par2Refs, func(i, j int) bool { return par2Refs[i].FileSize < par2Refs[j].FileSize })

	cache := newArticleCache(resolveCacheCap)
	var par2Files []SetFile
	var streams []io.Reader
	for _, ref := range par2Refs {
		sf := SetFile{Length: uint64(ref.FileSize)}
		for _, seg := range ref.SegmentData {
			sf.Articles = append(sf.Articles, Article{
				MessageID: normalizeMsgID(seg.Id),
				Size:      seg.SegmentSize,
			})
		}
		par2Files = append(par2Files, sf)
		streams = append(streams, newLazyFileReader(ctx, fetch, sf, cache))
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
	if err := ratioPrecheck(store.Files, par2Files, dead, caps); err != nil {
		return nil, err
	}

	idx, err := par2.ParseIndex(streams)
	if err != nil {
		return nil, fmt.Errorf("%w: parse PAR2 set: %v", ErrUnrepairable, err)
	}
	dropDeadRecovery(idx, par2Files, dead)

	// 2 + 3. Match recovery-set members to NzbStore entries and size articles.
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

// statSweep bulk-checks article liveness before planning, when the fetcher
// supports it, and folds confirmed misses into dead. STATs carry no body, so
// the sweep costs round trips only — a fraction of a single article download —
// and buys an exact damage picture: caps and recovery-count verdicts become
// accurate at plan time, and the payload sweep stops tripping over
// surprise-dead articles (each of which costs a full replan-and-retry cycle).
func statSweep(ctx context.Context, fetch ArticleFetcher, ids []string, dead map[string]bool) error {
	stater, ok := fetch.(ArticleStater)
	if !ok {
		return nil
	}
	unknown := make([]string, 0, len(ids))
	for _, id := range ids {
		if !dead[id] {
			unknown = append(unknown, id)
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	missing, err := stater.StatIDs(ctx, unknown)
	if err != nil {
		return fmt.Errorf("par2repair: liveness sweep: %w", err)
	}
	for id := range missing {
		dead[id] = true
	}
	return nil
}

// releaseArticleIDs collects every article ID of the release: all NzbStore
// segments plus the PAR2 set's articles (which metadata records separately).
func releaseArticleIDs(store *metapb.NzbStore, par2Files []SetFile) []string {
	var ids []string
	for _, f := range store.Files {
		for _, seg := range f.Segments {
			ids = append(ids, normalizeMsgID(seg.Id))
		}
	}
	for _, f := range par2Files {
		for _, a := range f.Articles {
			ids = append(ids, a.MessageID)
		}
	}
	return ids
}

// ratioPrecheckMargin keeps the pre-parse ratio check conservative: it works
// from encoded segment bytes, which only approximate the decoded sizes
// BuildPlan measures (yEnc overhead mostly cancels in the ratio). The margin
// ensures borderline releases fall through to BuildPlan's exact check instead
// of being rejected on an estimate.
const ratioPrecheckMargin = 1.05

// ratioPrecheck rejects damage far above the repair-ratio cap right after the
// liveness sweep — before the PAR2 set is parsed or any article body is
// downloaded. The estimate needs only the NZB layout: per damaged content
// file, dead segment bytes over the file's total segment bytes. PAR2 files
// (identified by their articles) are excluded, mirroring BuildPlan's ratio
// over recovery-set members. The error matches BuildPlan's exact-check
// message so callers and the UI treat both identically.
func ratioPrecheck(files []*metapb.NzbFileEntry, par2Files []SetFile, dead map[string]bool, caps Caps) error {
	if caps.MaxRepairRatio <= 0 || len(dead) == 0 {
		return nil
	}
	par2IDs := map[string]bool{}
	for _, f := range par2Files {
		for _, a := range f.Articles {
			par2IDs[a.MessageID] = true
		}
	}
	var missingBytes, damagedFileBytes int64
	for _, f := range files {
		if len(f.Segments) == 0 || par2IDs[normalizeMsgID(f.Segments[0].Id)] {
			continue
		}
		var fileBytes, deadBytes int64
		for _, seg := range f.Segments {
			fileBytes += seg.Bytes
			if dead[normalizeMsgID(seg.Id)] {
				deadBytes += seg.Bytes
			}
		}
		if deadBytes > 0 {
			missingBytes += deadBytes
			damagedFileBytes += fileBytes
		}
	}
	if damagedFileBytes == 0 {
		return nil
	}
	if ratio := float64(missingBytes) / float64(damagedFileBytes); ratio > caps.MaxRepairRatio*ratioPrecheckMargin {
		return fmt.Errorf("%w: damage ratio %.4f exceeds max_repair_ratio %.4f",
			ErrUnrepairable, ratio, caps.MaxRepairRatio)
	}
	return nil
}

// dropDeadRecovery removes recovery slice refs whose payload overlaps a dead
// article. ParseIndex seeks past recovery payloads without fetching them, so
// a ref can parse fine while its payload is gone; counting such refs would
// make BuildPlan promise recovery slices the job can never fetch.
func dropDeadRecovery(idx *par2.Index, par2Files []SetFile, dead map[string]bool) {
	if len(dead) == 0 {
		return
	}
	sliceSize := int64(idx.SliceSize)
	live := make([]par2.RecoverySliceRef, 0, len(idx.Recovery))
	for _, ref := range idx.Recovery {
		if payloadArticlesLive(par2Files[ref.FileIndex], ref.BodyOffset, sliceSize, dead) {
			live = append(live, ref)
		}
	}
	idx.Recovery = live
}

// payloadArticlesLive reports whether every article overlapping the payload
// range [off, off+n) of a file's concatenated articles is live.
func payloadArticlesLive(f SetFile, off, n int64, dead map[string]bool) bool {
	var artStart int64
	for _, a := range f.Articles {
		artEnd := artStart + a.Size
		if artEnd > off && artStart < off+n && dead[a.MessageID] {
			return false
		}
		artStart = artEnd
		if artStart >= off+n {
			break
		}
	}
	return true
}

// matchSetFiles pairs recovery-set FileDescs with NzbStore entries.
func matchSetFiles(
	ctx context.Context,
	idx *par2.Index,
	store *metapb.NzbStore,
	dead map[string]bool,
	fetch ArticleFetcher,
	cache *articleCache,
) ([]SetFile, error) {
	used := make([]bool, len(store.Files))
	var out []SetFile
	var matched []matchedFile

	for _, id := range idx.RecoveryIDs {
		fd := idx.Files[id]
		entry := -1
		// Pass 1: filename appears in the subject.
		for i, f := range store.Files {
			if !used[i] && fd.Name != "" && strings.Contains(f.Subject, fd.Name) {
				entry = i
				break
			}
		}
		// Pass 2: Hash16k of the entry's first bytes.
		if entry < 0 {
			for i, f := range store.Files {
				if used[i] || len(f.Segments) == 0 || dead[normalizeMsgID(f.Segments[0].Id)] {
					continue
				}
				prefix, err := filePrefix(ctx, fetch, f, cache)
				if err != nil {
					continue // dead or unreadable head article: cannot match by content
				}
				if bytes.HasPrefix(prefix, []byte("PAR2\x00PKT")) {
					continue // a PAR2 file, never a recovery-set member
				}
				if hash16k(prefix, int64(fd.Length)) == fd.Hash16k {
					entry = i
					break
				}
			}
		}
		if entry < 0 {
			return nil, fmt.Errorf("%w: recovery-set member %q not found in NZB", ErrUnrepairable, fd.Name)
		}
		used[entry] = true
		matched = append(matched, matchedFile{id: id, entry: store.Files[entry]})
	}

	// Pass 1: size every file that still has a live article to probe, and
	// remember the part size the release was posted with.
	out = make([]SetFile, len(matched))
	var releasePartSize int64
	var deferred []int
	for i, m := range matched {
		sf, partSize, err := sizeArticles(ctx, idx, m.id, m.entry, dead, fetch, cache, 0)
		if errors.Is(err, errNoLiveArticle) {
			deferred = append(deferred, i)
			continue
		}
		if err != nil {
			return nil, err
		}
		if releasePartSize == 0 && partSize > 0 {
			releasePartSize = partSize
		}
		out[i] = sf
	}

	// Pass 2: a volume with no live article at all is exactly what PAR2 repair
	// is for. Its decoded part size cannot be probed, so borrow the release's —
	// usenet posts split every file of a release at one uniform size. The
	// borrowed layout is checked against the file's exact PAR2 length here, and
	// every rebuilt slice is verified against its PAR2 MD5 before being stored,
	// so a wrong guess fails the repair rather than corrupting it.
	for _, i := range deferred {
		if releasePartSize == 0 {
			fd := idx.Files[matched[i].id]
			return nil, fmt.Errorf("%w: no live article anywhere in the release to derive the part size (every article of %q and its siblings is missing)",
				ErrUnrepairable, fd.Name)
		}
		sf, _, err := sizeArticles(ctx, idx, matched[i].id, matched[i].entry, dead, fetch, cache, releasePartSize)
		if err != nil {
			return nil, err
		}
		out[i] = sf
	}
	return out, nil
}

// matchedFile pairs a recovery-set member with the NZB entry it resolved to.
type matchedFile struct {
	id    [16]byte
	entry *metapb.NzbFileEntry
}

// errNoLiveArticle marks a file whose every article is missing, so its part
// size must come from elsewhere in the release.
var errNoLiveArticle = errors.New("par2repair: no live article to probe")

// sizeArticles derives per-article decoded sizes for one recovery-set member
// from the uniform part size of its first live article. When no article is
// live, hint (the release-wide part size, if already known) is used instead;
// a zero hint yields errNoLiveArticle so the caller can retry once it knows
// one. It returns the part size it used, or 0 when the file is a single
// article and therefore says nothing about the release's part size.
func sizeArticles(
	ctx context.Context,
	idx *par2.Index,
	fileID [16]byte,
	entry *metapb.NzbFileEntry,
	dead map[string]bool,
	fetch ArticleFetcher,
	cache *articleCache,
	hint int64,
) (SetFile, int64, error) {
	fd := idx.Files[fileID]
	n := len(entry.Segments)
	length := int64(fd.Length)
	sf := SetFile{FileID: fileID, Length: fd.Length}

	if n == 0 {
		return sf, 0, fmt.Errorf("%w: file %q has no segments in NZB", ErrUnrepairable, fd.Name)
	}

	partSize := length // single-article fallback: the whole file
	derived := int64(0)
	if n > 1 {
		partSize = -1
		for _, seg := range entry.Segments {
			msgID := normalizeMsgID(seg.Id)
			if dead[msgID] {
				continue
			}
			payload, err := fetchCached(ctx, fetch, msgID, cache)
			if err != nil {
				if errors.Is(err, nntppool.ErrArticleNotFound) {
					dead[msgID] = true // discovered dead while probing
					continue
				}
				return sf, 0, fmt.Errorf("par2repair: probe article %s: %w", msgID, err)
			}
			partSize = int64(len(payload))
			break
		}
		if partSize <= 0 {
			if hint <= 0 {
				return sf, 0, errNoLiveArticle
			}
			partSize = hint
		} else {
			derived = partSize
		}
		// The probe may have hit the (smaller) final article; a uniform part
		// size must satisfy (n-1)*partSize < length <= n*partSize. This is also
		// what validates a borrowed hint.
		if int64(n-1)*partSize >= length || int64(n)*partSize < length {
			return sf, 0, fmt.Errorf("%w: part size %d inconsistent with %d articles of %d bytes in %q",
				ErrUnrepairable, partSize, n, length, fd.Name)
		}
	}

	var off int64
	for i, seg := range entry.Segments {
		msgID := normalizeMsgID(seg.Id)
		size := partSize
		if i == n-1 {
			size = length - off
		}
		sf.Articles = append(sf.Articles, Article{
			MessageID: msgID,
			Size:      size,
			Dead:      dead[msgID],
		})
		off += size
	}
	if off != length {
		return sf, 0, fmt.Errorf("%w: article sizes for %q sum to %d, want %d", ErrUnrepairable, fd.Name, off, length)
	}
	return sf, derived, nil
}

// filePrefix returns the first up-to-16KiB of a store file's content.
func filePrefix(ctx context.Context, fetch ArticleFetcher, entry *metapb.NzbFileEntry, cache *articleCache) ([]byte, error) {
	payload, err := fetchCached(ctx, fetch, normalizeMsgID(entry.Segments[0].Id), cache)
	if err != nil {
		return nil, err
	}
	if len(payload) > 16384 {
		return payload[:16384], nil
	}
	return payload, nil
}

// hash16k computes the PAR2 Hash16k: MD5 of the first 16KiB of the file,
// zero-padded when the file itself is shorter (matching par2gen; files this
// small are rare in practice).
func hash16k(prefix []byte, fileLength int64) [16]byte {
	if fileLength >= 16384 && len(prefix) >= 16384 {
		return md5.Sum(prefix[:16384])
	}
	padded := make([]byte, 16384)
	copy(padded, prefix)
	return md5.Sum(padded)
}

// resolveCacheCap bounds the resolve/job article caches by entry count. The
// caches only exist to spare adjacent readers a refetch, so a small window is
// enough; without a cap a large PAR2 set would pin its header articles on the
// heap for the whole job.
const resolveCacheCap = 64

// articleCache is a FIFO-bounded article payload cache.
type articleCache struct {
	cap   int
	data  map[string][]byte
	order []string
}

func newArticleCache(cap int) *articleCache {
	return &articleCache{cap: cap, data: map[string][]byte{}}
}

func (c *articleCache) get(id string) ([]byte, bool) {
	data, ok := c.data[id]
	return data, ok
}

func (c *articleCache) put(id string, data []byte) {
	if _, ok := c.data[id]; ok {
		return
	}
	c.data[id] = data
	c.order = append(c.order, id)
	for len(c.order) > c.cap {
		delete(c.data, c.order[0])
		c.order = c.order[1:]
	}
}

func fetchCached(ctx context.Context, fetch ArticleFetcher, msgID string, cache *articleCache) ([]byte, error) {
	if data, ok := cache.get(msgID); ok {
		return data, nil
	}
	data, err := fetch.Fetch(ctx, msgID)
	if err != nil {
		return nil, err
	}
	cache.put(msgID, data)
	return data, nil
}

// normalizeMsgID strips optional angle brackets so NzbStore ids ("abc@x") and
// SegmentData ids ("<abc@x>") compare equal. Patch-store keys use the
// normalized form.
func normalizeMsgID(id string) string {
	return strings.Trim(id, "<>")
}

// lazyFileReader is a seekable reader over a file's concatenated article
// payloads, fetching articles only when their bytes are actually read.
// ParseIndex's seek-aware skips make scanning a PAR2 volume cost roughly one
// article per packet header instead of the whole file.
type lazyFileReader struct {
	ctx   context.Context
	fetch ArticleFetcher
	file  SetFile
	cache *articleCache
	pos   int64
	size  int64
}

func newLazyFileReader(ctx context.Context, fetch ArticleFetcher, file SetFile, cache *articleCache) *lazyFileReader {
	return &lazyFileReader{
		ctx: ctx, fetch: fetch, file: file, cache: cache,
		size: articleSizeSum(file.Articles),
	}
}

func (l *lazyFileReader) Read(p []byte) (int, error) {
	if l.pos >= l.size {
		return 0, io.EOF
	}
	n := int64(len(p))
	if l.pos+n > l.size {
		n = l.size - l.pos
	}
	// Locate and fetch only the articles overlapping [pos, pos+n).
	var artStart int64
	read := 0
	for _, a := range l.file.Articles {
		artEnd := artStart + a.Size
		if artEnd > l.pos && artStart < l.pos+n {
			data, err := fetchCached(l.ctx, l.fetch, a.MessageID, l.cache)
			if err != nil {
				return read, err
			}
			from := max(l.pos, artStart)
			to := min(l.pos+n, artEnd)
			copy(p[from-l.pos:to-l.pos], data[from-artStart:to-artStart])
			read = int(to - l.pos)
		}
		artStart = artEnd
		if artStart >= l.pos+n {
			break
		}
	}
	l.pos += int64(read)
	return read, nil
}

func (l *lazyFileReader) Seek(offset int64, whence int) (int64, error) {
	var abs int64
	switch whence {
	case io.SeekStart:
		abs = offset
	case io.SeekCurrent:
		abs = l.pos + offset
	case io.SeekEnd:
		abs = l.size + offset
	default:
		return 0, fmt.Errorf("par2repair: invalid seek whence %d", whence)
	}
	if abs < 0 {
		return 0, fmt.Errorf("par2repair: negative seek position")
	}
	l.pos = abs
	return abs, nil
}
