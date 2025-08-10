//go:generate mockgen -source=./cipher.go -destination=./cipher_mock.go -package=encryption Cipher

package encryption

import (
	"context"
	"io"

	"github.com/javi11/altmount/internal/utils"
)

type CipherType string

const (
	// The rclone crypt cipher type, which will encrypt all the file using a password, salt.
	RCloneCipherType CipherType = "rclone"
	// @deprecated, The headers crypt cipher type, which will encrypt the first 750000 bytes and the last 750000 bytes of the file
	HeadersCipherType CipherType = "headers"
	// The none cipher type, which will not encrypt the file
	NoneCipherType CipherType = "none"
	// The rar crypt cipher type, which will read rared files
	RarCipherType CipherType = "rar"
)

type Cipher interface {
	OverheadSize(fileSize int64) int64
	EncryptedSize(fileSize int64) int64
	DecryptedSize(encryptedFileSize int64) (int64, error)
	Open(
		ctx context.Context,
		rh *utils.RangeHeader,
		metadata map[string]string,
		getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
	) (io.ReadCloser, error)
	Name() CipherType
	Reload(cfg *Config) error
}
