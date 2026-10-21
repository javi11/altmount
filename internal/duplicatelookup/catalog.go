package duplicatelookup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/nzbparser"
	"github.com/kipsilabs/altmount/internal/database"
	"github.com/kipsilabs/altmount/internal/holes"
	"github.com/kipsilabs/altmount/internal/metadata"
	pb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/nzb"
	"github.com/spf13/afero"
)

// Catalog persists fingerprints while refreshing local metadata at most once a minute.
// It never downloads file contents until an exact name and size candidate exists.
type Catalog struct {
	meta        *metadata.MetadataService
	repo        *database.HealthRepository
	open        func(context.Context, string) (afero.File, error)
	gate        chan struct{}
	refreshedAt time.Time // protected by gate, as are all catalog operations
	scanned     map[string]catalogStamp
}

type catalogStamp struct {
	metadata  os.FileInfo
	store     os.FileInfo
	storePath string
}

func sameStamp(a, b os.FileInfo) bool {
	return a != nil && b != nil && os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

func NewCatalog(meta *metadata.MetadataService, repo *database.HealthRepository, open func(context.Context, string) (afero.File, error)) *Catalog {
	return &Catalog{meta: meta, repo: repo, open: open, gate: make(chan struct{}, 1), scanned: make(map[string]catalogStamp)}
}

type candidate struct {
	database.DuplicateCandidate
	xml       []byte
	storePath string
}

func (c *Catalog) inspect(ctx context.Context, path string) (*candidate, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	raw, e := os.ReadFile(c.meta.GetMetadataFilePath(path))
	if os.IsNotExist(e) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	m, e := c.meta.ReadFileMetadata(path)
	if e != nil {
		return nil, e
	}
	if m == nil {
		return nil, nil
	}
	if m.StoreRef == "" || m.Status != pb.FileStatus_FILE_STATUS_HEALTHY || m.Encryption != pb.Encryption_NONE || m.Password != "" || m.Salt != "" || len(m.AesKey) > 0 || len(m.AesIv) > 0 || len(m.NestedSources) > 0 || len(m.SharedOuterSources) > 0 || len(m.ClipBoundaries) > 0 || len(m.KnownHoles) > 0 {
		return nil, nil
	}
	// Archive members typically contain a partial first or last segment. Reject
	// those before decompressing the source release store again.
	for _, segment := range m.SegmentData {
		if segment.StartOffset != 0 || segment.EndOffset != segment.SegmentSize-1 {
			return nil, nil
		}
	}
	store, e := c.meta.Store().ReadStoreUncached(m.StoreRef)
	if e != nil {
		return nil, e
	}
	var data *pb.NzbFileEntry
	var sourceName string
	for _, f := range store.Files {
		if len(f.Segments) == 0 || len(f.Groups) == 0 {
			return nil, nil
		}
		for i, segment := range f.Segments {
			if segment.Id == "" || holes.IsPlaceholderID(segment.Id) || segment.Bytes <= 0 || segment.Number != int32(i+1) {
				return nil, nil
			}
		}
		subject, e := nzbparser.ParseSubject(f.Subject)
		if e != nil || subject.Filename == "" {
			return nil, nil
		}
		name := subject.Filename
		if strings.HasSuffix(strings.ToLower(name), ".par2") {
			continue
		}
		if data != nil {
			return nil, nil
		}
		data = f
		sourceName = name
	}
	if data == nil || archiveName(sourceName) {
		return nil, nil
	}
	if len(m.SegmentData) != len(data.Segments) {
		return nil, nil
	}
	var total int64
	for i, s := range m.SegmentData {
		if s.Id != data.Segments[i].Id || s.SegmentSize <= 0 || s.StartOffset != 0 || s.EndOffset != s.SegmentSize-1 || s.SegmentSize > m.FileSize-total {
			return nil, nil
		}
		total += s.SegmentSize
	}
	if total != m.FileSize {
		return nil, nil
	}
	name := m.OriginalFilename
	if name == "" {
		name = sourceName
	}
	if Validate(Request{Algorithm: Algorithm, Name: name, Size: m.FileSize, Hash: strings.Repeat("0", 64)}) != nil {
		return nil, nil
	}
	storeRaw, e := os.ReadFile(m.StoreRef)
	if e != nil {
		return nil, e
	}
	h := sha256.New()
	h.Write(raw)
	h.Write(storeRaw)
	return &candidate{DuplicateCandidate: database.DuplicateCandidate{Path: path, Name: name, Size: m.FileSize, Identity: hex.EncodeToString(h.Sum(nil))}, xml: nzb.BuildNZB(store), storePath: m.StoreRef}, nil
}
func archiveName(name string) bool {
	n := strings.ToLower(name)
	ext := filepath.Ext(n)
	if len(ext) == 4 && ext[1] == 'r' && ext[2] >= '0' && ext[2] <= '9' && ext[3] >= '0' && ext[3] <= '9' {
		return true
	}
	for _, ext := range []string{".rar", ".7z", ".zip", ".tar", ".gz", ".bz2", ".xz"} {
		if strings.HasSuffix(n, ext) {
			return true
		}
	}
	return false
}
func (c *Catalog) refresh(ctx context.Context) error {
	if !c.refreshedAt.IsZero() && time.Since(c.refreshedAt) < time.Minute {
		return nil
	}
	root := c.meta.GetMetadataDirectoryPath("")
	seen := make(map[string]bool)
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			if p == root && os.IsNotExist(e) {
				return nil
			}
			return e
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if d.IsDir() {
			if p != root && strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || !strings.HasSuffix(d.Name(), ".meta") {
			return nil
		}
		rel, e := filepath.Rel(root, p)
		if e != nil {
			return e
		}
		path := filepath.ToSlash(strings.TrimSuffix(rel, ".meta"))
		seen[path] = true
		info, e := d.Info()
		if e != nil {
			return e
		}
		if previous, ok := c.scanned[path]; ok && sameStamp(previous.metadata, info) {
			storeInfo, statErr := os.Stat(previous.storePath)
			if statErr == nil && sameStamp(previous.store, storeInfo) {
				return nil
			}
		}
		entry, e := c.inspect(ctx, path)
		if e != nil {
			return fmt.Errorf("inspect %s: %w", path, e)
		}
		if entry == nil {
			delete(c.scanned, path)
			return c.repo.DeleteDuplicateCandidate(ctx, path)
		}
		if e = c.repo.UpsertDuplicateCandidate(ctx, entry.DuplicateCandidate); e != nil {
			return e
		}
		storeInfo, e := os.Stat(entry.storePath)
		if e != nil {
			return e
		}
		c.scanned[path] = catalogStamp{metadata: info, store: storeInfo, storePath: entry.storePath}
		return nil
	})
	if err == nil {
		for path := range c.scanned {
			if !seen[path] {
				if err = c.repo.DeleteDuplicateCandidate(ctx, path); err != nil {
					return err
				}
				delete(c.scanned, path)
			}
		}
		c.refreshedAt = time.Now()
	}
	return err
}

type contextReader struct {
	ctx context.Context
	r   io.ReaderAt
}

func (r contextReader) ReadAt(p []byte, off int64) (int, error) {
	if e := r.ctx.Err(); e != nil {
		return 0, e
	}
	return r.r.ReadAt(p, off)
}
func (c *Catalog) Lookup(ctx context.Context, req Request) ([]byte, error) {
	if e := Validate(req); e != nil {
		return nil, e
	}
	select {
	case c.gate <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.gate }()
	if e := c.refresh(ctx); e != nil {
		return nil, e
	}
	entries, e := c.repo.DuplicateCandidates(ctx, req.Name, req.Size)
	if e != nil {
		return nil, e
	}
	for _, entry := range entries {
		current, e := c.inspect(ctx, entry.Path)
		if e != nil {
			return nil, e
		}
		if current == nil || current.Name != req.Name || current.Size != req.Size {
			if e = c.repo.DeleteDuplicateCandidate(ctx, entry.Path); e != nil {
				return nil, e
			}
			continue
		}
		health, e := c.repo.GetFileHealth(ctx, entry.Path)
		if e != nil {
			return nil, e
		}
		if health != nil && health.Status != database.HealthStatusHealthy {
			continue
		}
		hash := entry.Hash
		if current.Identity != entry.Identity {
			hash = ""
			if e = c.repo.UpsertDuplicateCandidate(ctx, current.DuplicateCandidate); e != nil {
				return nil, e
			}
		}
		if hash == "" {
			f, e := c.open(ctx, entry.Path)
			if e != nil {
				return nil, e
			}
			hash, e = Fingerprint(contextReader{ctx: ctx, r: f}, req.Size)
			closeErr := f.Close()
			if e != nil {
				return nil, e
			}
			if closeErr != nil {
				return nil, closeErr
			}
			again, e := c.inspect(ctx, entry.Path)
			if e != nil {
				return nil, e
			}
			if again == nil || again.Identity != current.Identity {
				return nil, fmt.Errorf("candidate changed while fingerprinting")
			}
			if e = c.repo.SetDuplicateFingerprint(ctx, entry.Path, current.Identity, hash); e != nil {
				return nil, e
			}
		}
		if hash != req.Hash {
			continue
		}
		if e = ctx.Err(); e != nil {
			return nil, e
		}
		again, e := c.inspect(ctx, entry.Path)
		if e != nil {
			return nil, e
		}
		if again == nil || again.Identity != current.Identity {
			return nil, fmt.Errorf("candidate changed during lookup")
		}
		health, e = c.repo.GetFileHealth(ctx, entry.Path)
		if e != nil {
			return nil, e
		}
		if health != nil && health.Status != database.HealthStatusHealthy {
			continue
		}
		return again.xml, nil
	}
	return nil, ctx.Err()
}
