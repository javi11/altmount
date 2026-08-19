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

	cache := map[string][]byte{}
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
	idx, err := par2.ParseIndex(streams)
	if err != nil {
		return nil, fmt.Errorf("%w: parse PAR2 set: %v", ErrUnrepairable, err)
	}

	dead := map[string]bool{}
	for _, id := range deadSegmentIDs {
		if id != "" {
			dead[normalizeMsgID(id)] = true
		}
	}

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

// matchSetFiles pairs recovery-set FileDescs with NzbStore entries.
func matchSetFiles(
	ctx context.Context,
	idx *par2.Index,
	store *metapb.NzbStore,
	dead map[string]bool,
	fetch ArticleFetcher,
	cache map[string][]byte,
) ([]SetFile, error) {
	used := make([]bool, len(store.Files))
	var out []SetFile

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
				if used[i] || len(f.Segments) == 0 {
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

		sf, err := sizeArticles(ctx, idx, id, store.Files[entry], dead, fetch, cache)
		if err != nil {
			return nil, err
		}
		out = append(out, sf)
	}
	return out, nil
}

// sizeArticles derives per-article decoded sizes for one recovery-set member
// from the uniform part size of its first live article.
func sizeArticles(
	ctx context.Context,
	idx *par2.Index,
	fileID [16]byte,
	entry *metapb.NzbFileEntry,
	dead map[string]bool,
	fetch ArticleFetcher,
	cache map[string][]byte,
) (SetFile, error) {
	fd := idx.Files[fileID]
	n := len(entry.Segments)
	length := int64(fd.Length)
	sf := SetFile{FileID: fileID, Length: fd.Length}

	if n == 0 {
		return sf, fmt.Errorf("%w: file %q has no segments in NZB", ErrUnrepairable, fd.Name)
	}

	partSize := length // single-article fallback: the whole file
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
				return sf, fmt.Errorf("par2repair: probe article %s: %w", msgID, err)
			}
			partSize = int64(len(payload))
			break
		}
		if partSize <= 0 {
			return sf, fmt.Errorf("%w: no live article in %q to derive part size", ErrUnrepairable, fd.Name)
		}
		// The probe may have hit the (smaller) final article; a uniform part
		// size must satisfy (n-1)*partSize < length <= n*partSize.
		if int64(n-1)*partSize >= length || int64(n)*partSize < length {
			return sf, fmt.Errorf("%w: part size %d inconsistent with %d articles of %d bytes in %q",
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
		return sf, fmt.Errorf("%w: article sizes for %q sum to %d, want %d", ErrUnrepairable, fd.Name, off, length)
	}
	return sf, nil
}

// filePrefix returns the first up-to-16KiB of a store file's content.
func filePrefix(ctx context.Context, fetch ArticleFetcher, entry *metapb.NzbFileEntry, cache map[string][]byte) ([]byte, error) {
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

func fetchCached(ctx context.Context, fetch ArticleFetcher, msgID string, cache map[string][]byte) ([]byte, error) {
	if data, ok := cache[msgID]; ok {
		return data, nil
	}
	data, err := fetch.Fetch(ctx, msgID)
	if err != nil {
		return nil, err
	}
	cache[msgID] = data
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
	cache map[string][]byte
	pos   int64
	size  int64
}

func newLazyFileReader(ctx context.Context, fetch ArticleFetcher, file SetFile, cache map[string][]byte) *lazyFileReader {
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
