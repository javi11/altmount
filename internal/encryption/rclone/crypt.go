package rclone

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/utils"
)

var (
	ErrMissingPassword          = errors.New("password is required in metadata")
	ErrMissingSalt              = errors.New("salt is required in metadata")
	ErrMissingEncryptedFileSize = errors.New("cipher_file_size is required in metadata")
)

type rcloneCrypt struct {
	// Cipher to use for encrypting/decrypting
	cipher            *Cipher
	hasGlobalPassword bool
}

func NewRcloneCipher(
	config *encryption.Config,
) (*rcloneCrypt, error) {
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

	return &rcloneCrypt{
		cipher:            cipher,
		hasGlobalPassword: config.RclonePassword != "",
	}, nil
}

// Opens a new crypt session, until read is not called, the underlying usenet reader is not called
// this way we don't perform reads while fetching the modtime
func (o *rcloneCrypt) Open(
	ctx context.Context,
	rh *utils.RangeHeader,
	metadata map[string]string,
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
) (rc io.ReadCloser, err error) {
	log := slog.Default()
	encryptedFileSize, err := getEncryptedFileSize(metadata)
	if err != nil {
		return nil, ErrMissingEncryptedFileSize
	}

	password, _ := getPassword(metadata)
	salt, _ := getSalt(metadata)

	var offset, limit int64 = 0, -1
	if rh != nil {
		s, err := o.DecryptedSize(encryptedFileSize)
		if err != nil {
			return nil, err
		}

		if rh.End == s-1 {
			rh.End = -1
		}

		offset, limit = rh.Decode(s)
	}

	if password == "" && !o.hasGlobalPassword {
		log.WarnContext(ctx, "No password provided for rclone crypt.")

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
				reader, err := getReader(ctx, 0, encryptedFileSize)
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

			rh := *utils.FixRangeHeader(&utils.RangeHeader{
				Start: underlyingOffset,
				End:   end,
			}, encryptedFileSize)

			reader, err := getReader(ctx, rh.Start, rh.End)
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
		initReader: initReader,
		logger:     log,
	}, nil
}

func (o *rcloneCrypt) DecryptedSize(fileSize int64) (int64, error) {
	return o.cipher.DecryptedSize(fileSize)
}

func (o *rcloneCrypt) EncryptedSize(fileSize int64) int64 {
	return o.cipher.EncryptedSize(fileSize)
}

func (o *rcloneCrypt) OverheadSize(fileSize int64) int64 {
	return o.cipher.EncryptedSize(fileSize) - fileSize
}

func (o *rcloneCrypt) Name() encryption.CipherType {
	return encryption.RCloneCipherType
}

type reader struct {
	once       sync.Once
	rd         io.ReadCloser
	initReader func() (io.ReadCloser, error)
	logger     *slog.Logger
}

func (r *reader) Read(p []byte) (n int, err error) {
	r.once.Do(func() {
		r.rd, err = r.initReader()
		if err != nil && !errors.Is(err, context.Canceled) {
			r.logger.Error("Failed to to read rclone crypt file", "err", err)
		}
	})

	if err != nil {
		return 0, err
	}

	return r.rd.Read(p)
}

func (r *reader) Close() error {
	if r.rd != nil {
		return r.rd.Close()
	}

	return nil
}
