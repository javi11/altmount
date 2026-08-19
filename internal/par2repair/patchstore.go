package par2repair

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// PatchStore persists repaired article payloads, one file per article, keyed
// by message ID. It has no in-memory state: the directory is the store, so
// patches survive restarts and are visible to any instance over the same
// root. Writes are atomic (tmp + rename): a concurrent reader sees a miss or
// a complete payload, never partial data. Patches are regenerable by
// re-running repair, so deleting files under the root is always safe.
type PatchStore struct {
	root string
}

// NewPatchStore returns a store rooted at the given directory (created on
// first Put).
func NewPatchStore(root string) *PatchStore {
	return &PatchStore{root: root}
}

// ScratchDir returns the directory for large transient job files (solver
// arenas). Contents only matter while a job is running; the repair service
// wipes it at startup.
func (p *PatchStore) ScratchDir() string { return filepath.Join(p.root, ".scratch") }

// Put atomically writes the payload for a message ID.
func (p *PatchStore) Put(messageID string, payload []byte) error {
	if messageID == "" {
		return errors.New("par2repair: empty message ID")
	}
	path := p.path(messageID)
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("par2repair: create patch dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("par2repair: create patch temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename

	if _, err := tmp.Write(payload); err != nil {
		tmp.Close()
		return fmt.Errorf("par2repair: write patch: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("par2repair: sync patch: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("par2repair: close patch: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("par2repair: publish patch: %w", err)
	}
	return nil
}

// Get returns the repaired payload for a message ID, or (nil, false) on miss.
func (p *PatchStore) Get(messageID string) ([]byte, bool) {
	if messageID == "" {
		return nil, false
	}
	data, err := os.ReadFile(p.path(messageID))
	if err != nil {
		return nil, false
	}
	return data, true
}

// Has reports whether a patch exists for the message ID.
func (p *PatchStore) Has(messageID string) bool {
	if messageID == "" {
		return false
	}
	_, err := os.Stat(p.path(messageID))
	return err == nil
}

// Prune bounds the store's total on-disk size: when the sum of all *.patch
// files exceeds maxBytes, the oldest (by mtime) are deleted first until the
// total fits. maxBytes <= 0 disables pruning. Patches are regenerable by
// re-running repair, so eviction is always safe.
func (p *PatchStore) Prune(maxBytes int64) error {
	if maxBytes <= 0 {
		return nil
	}
	type patchFile struct {
		path  string
		size  int64
		mtime time.Time
	}
	var (
		files []patchFile
		total int64
	)
	err := filepath.WalkDir(p.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// A file deleted mid-walk (concurrent prune/repair) is fine.
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".patch" {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		files = append(files, patchFile{path: path, size: info.Size(), mtime: info.ModTime()})
		total += info.Size()
		return nil
	})
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil // nothing stored yet
		}
		return fmt.Errorf("par2repair: scan patch store: %w", err)
	}
	if total <= maxBytes {
		return nil
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mtime.Before(files[j].mtime) })
	for _, f := range files {
		if total <= maxBytes {
			break
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("par2repair: evict patch %s: %w", f.path, err)
		}
		total -= f.size
	}
	return nil
}

// path fans patches out into 256 subdirectories by the first hash byte to
// keep directory sizes small.
func (p *PatchStore) path(messageID string) string {
	sum := sha256.Sum256([]byte(messageID))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(p.root, name[:2], name+".patch")
}
