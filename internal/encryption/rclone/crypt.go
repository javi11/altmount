package rclone

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/altmount/internal/utils"
)

var (
	ErrMissingPassword          = errors.New("password is required in metadata")
	ErrMissingSalt              = errors.New("salt is required in metadata")
	ErrMissingEncryptedFileSize = errors.New("cipher_file_size is required in metadata")
	noRetryErrors               = []error{
		ErrorBadDecryptUTF8,
		ErrorBadDecryptControlChar,
		ErrorNotAMultipleOfBlocksize,
		ErrorTooShortAfterDecode,
		ErrorTooLongAfterDecode,
		ErrorEncryptedFileTooShort,
		ErrorEncryptedFileBadHeader,
		ErrorEncryptedBadMagic,
		ErrorEncryptedBadBlock,
		ErrorBadBase32Encoding,
		ErrorFileClosed,
		ErrorBadSeek,
	}
)

// RcloneCrypt handles rclone-style file encryption/decryption
type RcloneCrypt struct {
	// Cipher to use for encrypting/decrypting
	cipher            *Cipher
	hasGlobalPassword bool
}

func NewRcloneCipher(
	config *encryption.Config,
) (*RcloneCrypt, error) {
	cipher, err := NewCipher(
		NameEncryptionOff,
		config.RclonePassword,
		config.RcloneSalt,
		false,
		nil,
	)
	if err != nil {
		return nil, err
	}

	return &RcloneCrypt{
		cipher:            cipher,
		hasGlobalPassword: config.RclonePassword != "",
	}, nil
}

// Opens a new crypt session, until read is not called, the underlying usenet reader is not called
// this way we don't perform reads while fetching the modtime
func (o *RcloneCrypt) Open(
	ctx context.Context,
	rh *utils.RangeHeader,
	fileSize int64,
	password string,
	salt string,
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
) (rc io.ReadCloser, err error) {
	encryptedFileSize := o.EncryptedSize(fileSize)

	var offset, limit int64 = 0, -1
	if rh != nil {
		if rh.End == fileSize-1 {
			rh.End = -1
		}

		offset, limit = rh.Decode(fileSize)
	}

	if password == "" && !o.hasGlobalPassword {
		slog.WarnContext(ctx, "No password provided for rclone crypt.")

		return nil, ErrMissingPassword
	}

	var key *key
	if password != "" {
		key, err = GenerateKey(password, salt)
		if err != nil {
			return nil, err
		}
	}

	initReader := func() (io.ReadCloser, error) {
		rc, err = o.cipher.DecryptDataSeek(ctx, func(ctx context.Context, underlyingOffset, underlyingLimit int64) (io.ReadCloser, error) {
			if underlyingOffset == 0 && underlyingLimit < 0 {
				reader, err := getReader(ctx, 0, encryptedFileSize-1)
				if err != nil {
					return nil, err
				}

				return reader, nil
			}
			// Open stream with a range of underlyingOffset, underlyingLimit
			end := int64(-1)
			if underlyingLimit >= 0 {
				end = underlyingOffset + underlyingLimit - 1
				if end >= encryptedFileSize {
					end = -1
				}
			}

			// Convert end=-1 to actual end position for getReader
			// getReader expects inclusive end positions, not -1
			actualEnd := end
			if end == -1 {
				actualEnd = encryptedFileSize - 1
			}

			reader, err := getReader(ctx, underlyingOffset, actualEnd)
			if err != nil {
				return nil, err
			}

			return reader, nil
		}, offset, limit, key)
		if err != nil &&
			// this error can be caused by an EOF at connection level so a retry will fix it
			!errors.Is(err, ErrorEncryptedFileTooShort) {
			return nil, errors.Join(err, encryption.ErrCorruptedCrypt)
		}
		return rc, nil
	}

	return &reader{
		ctx:        ctx,
		initReader: initReader,
	}, nil
}

func (o *RcloneCrypt) DecryptedSize(fileSize int64) (int64, error) {
	return o.cipher.DecryptedSize(fileSize)
}

func (o *RcloneCrypt) EncryptedSize(fileSize int64) int64 {
	return EncryptedSize(fileSize)
}

func (o *RcloneCrypt) OverheadSize(fileSize int64) int64 {
	return EncryptedSize(fileSize) - fileSize
}

type reader struct {
	once       sync.Once
	rd         io.ReadCloser
	ctx        context.Context
	initReader func() (io.ReadCloser, error)
}

func (r *reader) Read(p []byte) (n int, err error) {
	r.once.Do(func() {
		r.rd, err = r.initReader()
	})

	if err != nil {
		return 0, err
	}

	if r.rd == nil {
		return 0, errors.New("rclone crypt reader not initialized")
	}

	if r.ctx.Err() != nil {
		return 0, r.ctx.Err()
	}

	n, err = r.rd.Read(p)
	if err != nil {
		for _, noRetryError := range noRetryErrors {
			if errors.Is(err, noRetryError) {
				return n, &usenet.DataCorruptionError{
					UnderlyingErr: err,
					BytesRead:     int64(n),
					NoRetry:       true,
				}
			}
		}

		return n, err
	}

	return n, nil
}

func (r *reader) Close() error {
	if r.rd != nil {
		return r.rd.Close()
	}

	return nil
}
