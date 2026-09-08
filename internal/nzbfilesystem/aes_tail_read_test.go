package nzbfilesystem

import (
	"bytes"
	"context"
	cryptoaes "crypto/aes"
	"crypto/cipher"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/kipsilabs/altmount/internal/encryption/aes"
	metapb "github.com/kipsilabs/altmount/internal/metadata/proto"
	"github.com/kipsilabs/altmount/internal/testsupport/fakepool"
	"github.com/kipsilabs/altmount/internal/testsupport/segments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newAESTestMVF builds an AES-CBC encrypted file whose plaintext length is
// `pad` bytes short of a block boundary, so the final ciphertext block extends
// past FileSize. The segments cover the whole padded ciphertext (as the RAR5
// and 7z importers store it) unless the caller trims them afterwards.
func newAESTestMVF(t *testing.T, n, segSize, pad int) (*MetadataVirtualFile, []byte) {
	t.Helper()
	total := n * segSize
	plainLen := total - pad
	plain := segments.FileBytes(n, segSize)[:plainLen]

	key := bytes.Repeat([]byte{0x42}, 32)
	iv := bytes.Repeat([]byte{0x24}, 16)
	block, err := cryptoaes.NewCipher(key)
	require.NoError(t, err)
	padded := append(append([]byte{}, plain...), bytes.Repeat([]byte{byte(pad)}, pad)...)
	ct := make([]byte, total)
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ct, padded)

	fp := fakepool.New()
	for i := range n {
		fp.SetBehavior(segments.MessageID(i), fakepool.SegmentBehavior{Bytes: ct[i*segSize : (i+1)*segSize]})
	}
	mvf := newTestMVF(t, context.Background(), fp, n, segSize, 4)
	mvf.meta.FileSize = int64(plainLen)
	mvf.meta.Encryption = metapb.Encryption_AES
	mvf.meta.AesKey = key
	mvf.meta.AesIv = iv
	mvf.aesCipher = aes.NewAesCipher()
	return mvf, plain
}

// readAllOrHang fails the test instead of hanging forever when the read path
// spins: the bug this guards against held mvf.mu in a busy loop.
func readAllOrHang(t *testing.T, r io.Reader) ([]byte, error) {
	t.Helper()
	type res struct {
		b   []byte
		err error
	}
	done := make(chan res, 1)
	go func() {
		b, err := io.ReadAll(r)
		done <- res{b, err}
	}()
	select {
	case r := <-done:
		return r.b, r.err
	case <-time.After(10 * time.Second):
		t.Fatal("read hung: the reader is spinning instead of returning")
		return nil, nil
	}
}

func TestAESTailReadReachesPaddedFinalBlock(t *testing.T) {
	const n, segSize, pad = 8, 64 << 10, 8
	mvf, plain := newAESTestMVF(t, n, segSize, pad)
	start := int64(len(plain) - 2000)

	_, err := mvf.Seek(start, io.SeekStart)
	require.NoError(t, err)

	got, err := readAllOrHang(t, mvf)
	require.NoError(t, err)
	assert.True(t, bytes.Equal(got, plain[start:]), "got %d bytes, want %d", len(got), len(plain)-int(start))
}

func TestReadReturnsErrorWhenReaderCannotReachRangeEnd(t *testing.T) {
	const n, segSize, pad = 8, 64 << 10, 8
	mvf, plain := newAESTestMVF(t, n, segSize, pad)
	// Truncated metadata: the padded final block is not addressable, so the
	// decryptor can never produce the last bytes. That must surface as an
	// error, never as a retry loop.
	last := mvf.meta.SegmentData[n-1]
	last.EndOffset -= pad
	start := int64(len(plain) - 2000)

	_, err := mvf.Seek(start, io.SeekStart)
	require.NoError(t, err)

	got, err := readAllOrHang(t, mvf)
	require.Error(t, err)
	assert.True(t, errors.Is(err, io.ErrUnexpectedEOF), "want ErrUnexpectedEOF, got %v", err)
	assert.True(t, bytes.HasPrefix(plain[start:], got), "delivered bytes must be a prefix of the plaintext")
}
