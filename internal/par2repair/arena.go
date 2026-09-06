package par2repair

import (
	"errors"
	"fmt"
	"os"
)

// arena hands out large byte buffers backed by a memory-mapped scratch file,
// so repair jobs whose solver buffers exceed the memory budget are paged by
// the OS instead of held on the heap. Buffers come back zeroed (fresh file
// pages) and stay valid until Close.
type arena struct {
	f    *os.File
	data []byte
	off  int
}

// newArena creates a scratch file of the given capacity under dir (created if
// missing) and maps it read-write.
func newArena(dir string, capacity int64) (*arena, error) {
	if capacity <= 0 || capacity != int64(int(capacity)) {
		return nil, fmt.Errorf("par2repair: invalid arena capacity %d", capacity)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("par2repair: create scratch dir: %w", err)
	}
	f, err := os.CreateTemp(dir, ".par2repair-*.mem")
	if err != nil {
		return nil, fmt.Errorf("par2repair: create arena file: %w", err)
	}
	cleanup := func() {
		_ = f.Close()
		_ = os.Remove(f.Name())
	}
	if err := f.Truncate(capacity); err != nil {
		cleanup()
		return nil, fmt.Errorf("par2repair: size arena file to %d bytes: %w", capacity, err)
	}
	data, err := mmapFile(f, int(capacity))
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("par2repair: map arena file: %w", err)
	}
	return &arena{f: f, data: data}, nil
}

// alloc returns the next n bytes of the mapping, zeroed, capped so appends
// cannot bleed into a neighbouring allocation.
func (a *arena) alloc(n int) ([]byte, error) {
	if n < 0 || a.off+n > len(a.data) {
		return nil, fmt.Errorf("par2repair: arena exhausted: need %d bytes, %d of %d left",
			n, len(a.data)-a.off, len(a.data))
	}
	buf := a.data[a.off : a.off+n : a.off+n]
	a.off += n
	return buf, nil
}

// Close unmaps and deletes the scratch file. All allocated buffers become
// invalid; touching one afterwards faults.
func (a *arena) Close() error {
	var errs []error
	if a.data != nil {
		errs = append(errs, munmapFile(a.data))
		a.data = nil
	}
	if a.f != nil {
		errs = append(errs, a.f.Close())
		errs = append(errs, os.Remove(a.f.Name()))
		a.f = nil
	}
	return errors.Join(errs...)
}
