package headers

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/utils"
)

const (
	encryptedSize = int64(750000)
)

var (
	ErrNonceSize       = errors.New("nonce must be 24 bytes")
	defaultSalt        = []byte{0xA8, 0x0D, 0xF4, 0x3A, 0x8F, 0xBD, 0x03, 0x08, 0xA7, 0xCA, 0xB8, 0x3E, 0x58, 0x1F, 0x86, 0xB1}
	ErrMissingPassword = errors.New("password is required in metadata")
	ErrMissingNonce    = errors.New("salt is required in metadata")
	ErrMissingFileSize = errors.New("file_size is required in metadata")
)

type headersCrypt struct {
	log *slog.Logger
}

/**
 * NewHeadersCipher encrypts the first 750000 bytes and the last 750000 bytes of the file
 * with the Salsa20 stream cipher. The middle part is not encrypted.
 * This allow to encrypt the file metadata and make the file not recognizable.
 **/
func NewHeadersCipher() (*headersCrypt, error) {
	return &headersCrypt{
		log: slog.Default(),
	}, nil
}

// Opens a new crypt session, until read is not called, the underlying usenet reader is not called
// this way we don't perform reads while fetching the modtime
func (o *headersCrypt) Open(
	ctx context.Context,
	rh *utils.RangeHeader,
	fileSize int64,
	password string,
	n string,
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
) (io.ReadCloser, error) {
	nc, err := getNonce(n)
	if err != nil {
		return nil, ErrMissingNonce
	}

	if len(nc) != fileNonceSize {
		return nil, ErrNonceSize
	}

	salt := string(defaultSalt)
	if rh == nil {
		rh = &utils.RangeHeader{
			Start: 0,
			End:   fileSize - 1,
		}
	}

	initReader := func() (io.Reader, io.Closer, error) {
		// Helper function to derive key
		key, err := deriveKey(password, salt)
		if err != nil {
			return nil, nil, err
		}

		uReader, err := getReader(ctx, rh.Start, rh.End)
		if err != nil {
			return nil, nil, err
		}

		if fileSize <= encryptedSize*2 {
			// Decrypt the whole file
			// Decrypt the entire range requested
			r := &reader{
				initReader: func() (io.Reader, io.Closer, error) {
					rd := decryptStream(ctx, key, nc, uReader)
					return rd, rd, nil
				},
				logger: o.log,
			}

			return r, r, nil
		}

		readers := make([]io.Reader, 0)
		closers := make([]io.Closer, 0)
		// First part: first 750000 bytes encrypted
		if rh.Start < encryptedSize {
			limit := encryptedSize
			if rh.End < encryptedSize {
				limit = rh.End + 1
			}
			limit -= rh.Start

			// Decrypt the first part
			r := &reader{
				initReader: func() (io.Reader, io.Closer, error) {
					rd := decryptStream(ctx, key, nc, io.LimitReader(uReader, limit))
					return rd, rd, nil
				},
				logger: o.log,
			}
			readers = append(readers, r)
			closers = append(closers, r)
		}

		// Second part: non-encrypted data
		middleStart := encryptedSize
		middleEnd := fileSize - encryptedSize
		if rh.Start < middleEnd && rh.End >= middleStart {
			start := max(rh.Start, middleStart)
			end := min(rh.End, middleEnd-1)

			readers = append(readers, io.LimitReader(uReader, end-start+1))
			closers = append(closers, uReader)
		}

		// Third part: last 750000 bytes encrypted
		if rh.End >= middleEnd {
			start := max(rh.Start, middleEnd)
			limit := int64(encryptedSize)
			if rh.End < fileSize {
				limit = rh.End - start + 1
			}

			// Decrypt the last part
			r := &reader{
				initReader: func() (io.Reader, io.Closer, error) {
					rd := decryptStream(ctx, key, nc, io.LimitReader(uReader, limit))
					return rd, rd, nil
				},
				logger: o.log,
			}
			readers = append(readers, r)
			closers = append(closers, r)
		}

		return io.MultiReader(readers...), &multiCloser{closers: closers}, nil
	}

	r := &reader{
		initReader: initReader,
		logger:     o.log,
	}

	return r, nil
}

func (o *headersCrypt) DecryptedSize(fileSize int64) (int64, error) {
	return fileSize, nil
}

func (o *headersCrypt) EncryptedSize(fileSize int64) int64 {
	return fileSize
}

func (o *headersCrypt) OverheadSize(_ int64) int64 {
	return 0
}

func (o *headersCrypt) Name() encryption.CipherType {
	return encryption.HeadersCipherType
}

type multiCloser struct {
	closers []io.Closer
}

func (m *multiCloser) Close() error {
	for _, closer := range m.closers {
		closer.Close()
	}

	return nil
}

func (o *headersCrypt) Reload(
	cfg *encryption.Config,
) error {
	return nil
}

type reader struct {
	once       sync.Once
	rd         io.Reader
	closer     io.Closer
	initReader func() (io.Reader, io.Closer, error)
	logger     *slog.Logger
}

func (r *reader) Read(p []byte) (n int, err error) {
	r.once.Do(func() {
		r.rd, r.closer, err = r.initReader()
		if err != nil {
			r.logger.Error("Failed to to read crypt file headers", "err", err)
		}
	})

	if err != nil {
		return 0, err
	}

	return r.rd.Read(p)
}

func (r *reader) Close() error {
	if r.closer != nil {
		return r.closer.Close()
	}

	return nil
}

// Helper functions
func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func min(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
