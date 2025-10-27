package aes

import (
	"context"
	"fmt"
	"io"

	"github.com/javi11/altmount/internal/encryption"
	"github.com/javi11/altmount/internal/utils"
)

// AesCipher implements the Cipher interface for AES-CBC encrypted archives
// Used for password-protected RAR, 7z, and other AES-encrypted archive formats
type AesCipher struct{}

// NewAesCipher creates a new AES cipher
func NewAesCipher() encryption.Cipher {
	return &AesCipher{}
}

// OverheadSize returns the encryption overhead for AES-CBC
// AES-CBC has minimal overhead (padding to block size)
func (c *AesCipher) OverheadSize(fileSize int64) int64 {
	// AES block size is 16 bytes
	blockSize := int64(16)
	// Calculate padding needed to reach block size boundary
	if fileSize%blockSize == 0 {
		return 0
	}
	return blockSize - (fileSize % blockSize)
}

// EncryptedSize calculates the encrypted size for a given plaintext size
func (c *AesCipher) EncryptedSize(fileSize int64) int64 {
	return fileSize + c.OverheadSize(fileSize)
}

// DecryptedSize calculates the decrypted size from encrypted size
func (c *AesCipher) DecryptedSize(encryptedFileSize int64) (int64, error) {
	// For AES-CBC, we can't know the exact size without decrypting
	// due to padding, but we can provide a maximum
	blockSize := int64(16)
	// Maximum plaintext is encrypted size minus one block (worst case padding)
	maxPlaintext := encryptedFileSize - blockSize
	if maxPlaintext < 0 {
		maxPlaintext = 0
	}
	return maxPlaintext, nil
}

// Open creates a decrypting reader for AES-encrypted data
// The password and salt parameters are not used for AES - the key and IV are provided through context
func (c *AesCipher) Open(
	ctx context.Context,
	rh *utils.RangeHeader,
	encryptedFileSize int64,
	password string, // Not used for AES - kept for interface compatibility
	salt string, // Not used for AES - kept for interface compatibility
	getReader func(ctx context.Context, start, end int64) (io.ReadCloser, error),
) (io.ReadCloser, error) {
	// Extract AES key and IV from context
	// These should be stored in the context by the caller
	key, ok := ctx.Value("aes_key").([]byte)
	if !ok || len(key) == 0 {
		return nil, fmt.Errorf("AES key not found in context")
	}

	iv, ok := ctx.Value("aes_iv").([]byte)
	if !ok || len(iv) == 0 {
		return nil, fmt.Errorf("AES IV not found in context")
	}

	// Calculate the decrypted size
	// This is approximate due to padding
	decryptedSize := encryptedFileSize

	// Wrap with AES decryption
	// The decrypt reader will lazily initialize the source reader when needed
	decryptReader, err := newAesDecryptReader(ctx, getReader, key, iv, decryptedSize)
	if err != nil {
		return nil, fmt.Errorf("failed to create AES decrypt reader: %w", err)
	}

	// If a range header is provided, seek to the requested position
	if rh != nil && rh.Start > 0 {
		_, err := decryptReader.Seek(rh.Start, io.SeekStart)
		if err != nil {
			decryptReader.Close()
			return nil, fmt.Errorf("failed to seek to start position: %w", err)
		}
	}

	return decryptReader, nil
}

// Name returns the cipher type identifier
func (c *AesCipher) Name() encryption.CipherType {
	return encryption.AesCipherType
}
