package headers

import (
	"context"
	"io"

	"golang.org/x/crypto/salsa20"
	"golang.org/x/crypto/scrypt"
)

const keyLen = 32 // Salsa20 key length is 32 bytes

func deriveKey(password string, salt string) ([keyLen]byte, error) {
	key, err := scrypt.Key([]byte(password), []byte(salt), 1<<15, 8, 1, keyLen)
	if err != nil {
		return [keyLen]byte{}, err
	}
	var keyArr [keyLen]byte
	copy(keyArr[:], key)

	return keyArr, nil
}

func encryptStream(ctx context.Context, key [32]byte, nc []byte, in io.Reader) io.ReadCloser {
	pr, pw := io.Pipe()

	go func() {
		buf := make([]byte, 4096)
		keystream := make([]byte, 4096)

		for {
			select {
			case <-ctx.Done():
				pw.Close()
				return
			default:
				n, err := in.Read(buf)
				if err != nil && err != io.EOF {
					pw.CloseWithError(err)
					return
				}
				if n == 0 {
					pw.Close()
					return
				}
				salsa20.XORKeyStream(keystream[:n], buf[:n], nc, &key)
				_, err = pw.Write(keystream[:n])
				if err != nil {
					pw.CloseWithError(err)
					return
				}
			}
		}
	}()

	return pr
}

func decryptStream(ctx context.Context, key [32]byte, nonce []byte, in io.Reader) io.ReadCloser {
	// For stream ciphers, decryption is the same operation as encryption.
	return encryptStream(ctx, key, nonce, in)
}
