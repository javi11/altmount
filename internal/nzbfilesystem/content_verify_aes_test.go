package nzbfilesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javi11/altmount/internal/contentverify"
	"github.com/javi11/altmount/internal/importer/parser/fileinfo"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// singleFileOpener adapts an already-open afero.File to contentverify.Opener,
// standing in for the thin NzbFilesystem/MetadataRemoteFile routing layer —
// decryption itself happens inside MetadataVirtualFile, which this test
// exercises directly and for real.
type singleFileOpener struct{ f afero.File }

func (o *singleFileOpener) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (afero.File, error) {
	return o.f, nil
}

// mkvPlaintext builds a plaintext buffer whose first bytes are a valid
// Matroska EBML header (with DocType "matroska") and the rest is
// deterministic filler, sized as a multiple of the AES block size so
// encryptCBC (defined in migration_encrypted_nested_test.go) accepts it.
func mkvPlaintext(size int) []byte {
	header := []byte{0x1A, 0x45, 0xDF, 0xA3, 0x42, 0x82, 0x88}
	header = append(header, []byte("matroska")...)
	out := make([]byte, size)
	copy(out, header)
	for i := len(header); i < size; i++ {
		out[i] = byte((i*7 + 13) % 251)
	}
	return out
}

// TestProbe_AESEncryptedRARContent confirms contentverify.Probe needs no
// special-casing for AES-encrypted content: MetadataVirtualFile already
// decrypts before Read returns bytes, so a valid inner media signature is
// detected exactly as for an unencrypted file.
func TestProbe_AESEncryptedRARContent(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()
	ms := metadata.NewMetadataService(root)

	plaintext := mkvPlaintext(512)
	fp := fakepool.New()
	segs := serveBytes(fp, encryptCBC(t, plaintext), 128)
	require.Len(t, segs, 4)

	vpath := filepath.Join("movies", "Encrypted.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize:      int64(len(plaintext)),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: filepath.Join(configDir, "encrypted.nzb"),
		Encryption:    metapb.Encryption_AES,
		AesKey:        testAESKey,
		AesIv:         testAESIV,
		SegmentData:   segs,
	}))

	meta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)

	mvf := mvfWithCipher(t, ctx, fp, "aes-probe", meta)
	opener := &singleFileOpener{f: mvf}

	require.True(t, fileinfo.IsVerifiableMediaFile(vpath))

	res := contentverify.Probe(ctx, opener, vpath, 5*time.Second)
	require.Equal(t, contentverify.ContentValid, res.Result, "AES-decrypted content with a valid signature must probe as ContentValid")
}
