package par2repair

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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

// path fans patches out into 256 subdirectories by the first hash byte to
// keep directory sizes small.
func (p *PatchStore) path(messageID string) string {
	sum := sha256.Sum256([]byte(messageID))
	name := hex.EncodeToString(sum[:])
	return filepath.Join(p.root, name[:2], name+".patch")
}
