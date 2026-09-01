package iso

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/javi11/altmount/internal/importer/filesystem"
	"github.com/javi11/altmount/internal/pool"
)

// NewISOReadSeeker creates an io.ReadSeeker backed by Usenet segments for the given
// ISOSource. When AesKey is non-nil the data is decrypted on-the-fly using AES-CBC.
// The returned io.Closer must be called to release resources.
func NewISOReadSeeker(
	ctx context.Context,
	src ISOSource,
	poolManager pool.Manager,
	maxPrefetch int,
	readTimeout time.Duration,
) (io.ReadSeeker, io.Closer, error) {
	entry := filesystem.DecryptingFileEntry{
		Filename:      src.Filename,
		Segments:      src.Segments,
		DecryptedSize: src.Size,
		AesKey:        src.AesKey,
		AesIV:         src.AesIV,
	}

	// Import-scoped segment cache: ISO structure parsing seeks back and forth
	// across volume descriptors and path tables, often re-reading leading
	// segments. Bounded and released (by dropping the reference) once the
	// caller closes the returned Closer.
	segStore := filesystem.NewImportSegmentCache(0)
	fsys := filesystem.NewDecryptingFileSystem(ctx, poolManager, []filesystem.DecryptingFileEntry{entry}, maxPrefetch, readTimeout, segStore)

	f, err := fsys.Open(src.Filename)
	if err != nil {
		return nil, nil, fmt.Errorf("iso: opening entry %q: %w", src.Filename, err)
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		_ = f.Close()
		return nil, nil, fmt.Errorf("iso: opened file does not implement io.ReadSeeker")
	}

	// The cache lives as long as the reader, so its effectiveness can only be
	// reported once the caller is done with it — not when this constructor
	// returns, which is before a single sector has been read.
	closer := closerFunc(func() error {
		err := f.Close()
		segStore.LogStats(ctx, nil, "iso-structure")
		return err
	})

	return rs, closer, nil
}

// closerFunc adapts a function to io.Closer.
type closerFunc func() error

func (fn closerFunc) Close() error { return fn() }
