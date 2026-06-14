package filesystem

import (
	"fmt"
	"path/filepath"

	"github.com/javi11/altmount/internal/importer/rarname"
)

// volumeIndex resolves a requested RAR/split volume filename to the actual stored
// filename when the two differ only in zero-padding width.
//
// rardecode computes each next volume name using a fixed digit width derived from
// the first volume (nextNewVolName), so a set numbered without consistent padding —
// e.g. "X.part01.rar" … "X.part09.rar" then "X.part010.rar" … "X.part0259.rar"
// (literally "part0" + the unpadded number) — makes rardecode request
// "X.part10.rar" after "X.part09.rar". That file does not exist (the real one is
// "X.part010.rar"), the exact-name lookup misses, and volume following stops at
// volume 9. Resolving by (set, scheme, volume number) instead lets the actual file
// be found regardless of its padding width, so the whole set is followed.
//
// The key includes the set base name so volumes from different sets in the same NZB
// cannot collide on the same scheme+number.
type volumeIndex struct {
	byNumber map[string]string // (setKey,scheme,volume) -> actual stored filename
}

// newVolumeIndex builds a number-keyed index over the given stored filenames.
// On duplicate keys the first name wins; exact-name lookups are handled by the
// caller's own map, so this index only ever serves width-mismatch fallbacks.
func newVolumeIndex(names []string) volumeIndex {
	vi := volumeIndex{byNumber: make(map[string]string, len(names))}
	for _, n := range names {
		if key, ok := volumeKey(n); ok {
			if _, exists := vi.byNumber[key]; !exists {
				vi.byNumber[key] = n
			}
		}
	}
	return vi
}

// resolve returns the actual stored filename for a requested volume name, or
// ("", false) when the name is not a recognized volume or no number-keyed match
// exists.
func (vi volumeIndex) resolve(name string) (string, bool) {
	key, ok := volumeKey(name)
	if !ok {
		return "", false
	}
	actual, ok := vi.byNumber[key]
	return actual, ok
}

// volumeKey derives the (set base, scheme, volume number) key for a filename,
// width-independent, or ("", false) for non-volume names.
func volumeKey(filename string) (string, bool) {
	base := filepath.Base(filename)
	setKey, ok := rarname.SetKey(base)
	if !ok {
		return "", false
	}
	scheme, num, ok := rarname.VolumeNumber(base)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s\x00%d\x00%d", setKey, int(scheme), num), true
}
