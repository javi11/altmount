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

	fsys := filesystem.NewDecryptingFileSystem(ctx, poolManager, []filesystem.DecryptingFileEntry{entry}, maxPrefetch, readTimeout)

	f, err := fsys.Open(src.Filename)
	if err != nil {
		return nil, nil, fmt.Errorf("iso: opening entry %q: %w", src.Filename, err)
	}

	rs, ok := f.(io.ReadSeeker)
	if !ok {
		_ = f.Close()
		return nil, nil, fmt.Errorf("iso: opened file does not implement io.ReadSeeker")
	}

	return rs, f, nil
}
