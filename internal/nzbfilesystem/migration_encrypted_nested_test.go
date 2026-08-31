package nzbfilesystem

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"path/filepath"
	"testing"

	aescipher "github.com/javi11/altmount/internal/encryption/aes"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/altmount/internal/testsupport/segments"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// migration_encrypted_nested_test.go extends the streaming migration coverage to
// the two shapes that carry the most migration risk:
//
//   - AES-encrypted files, where aes_key / aes_iv must survive the rewrite and
//     the decrypting reader must still see byte-identical ciphertext.
//   - Nested-RAR files, where nested_sources[].segments become segment_refs and
//     — the interesting part — a legacy shared_outer_sources dedup is *dissolved*
//     by the migration, so each nested source becomes self-contained. That is a
//     structural change to the metadata, not just a re-encoding, which makes it
//     the case most likely to break silently.
//
// Every test streams real bytes through MetadataVirtualFile before and after the
// real MigrationWorker runs, and compares them.

var (
	testAESKey = []byte("0123456789abcdef") // AES-128
	testAESIV  = []byte("fedcba9876543210")
)

// encryptCBC returns AES-CBC ciphertext for block-aligned plaintext.
func encryptCBC(t testing.TB, plaintext []byte) []byte {
	t.Helper()
	require.Zero(t, len(plaintext)%aescipher.BlockSize, "fixture plaintext must be block aligned")
	block, err := aes.NewCipher(testAESKey)
	require.NoError(t, err)
	out := make([]byte, len(plaintext))
	cipher.NewCBCEncrypter(block, testAESIV).CryptBlocks(out, plaintext)
	return out
}

// plainPattern builds deterministic, non-repeating plaintext of size n.
func plainPattern(n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = byte((i*7 + 13) % 251)
	}
	return out
}

// serveBytes splits payload across n segments of segSize and teaches the
// fakepool to serve each one under its canonical message-ID.
func serveBytes(fp *fakepool.Client, payload []byte, segSize int) []*metapb.SegmentData {
	var segs []*metapb.SegmentData
	for i := 0; i*segSize < len(payload); i++ {
		end := (i + 1) * segSize
		if end > len(payload) {
			end = len(payload)
		}
		chunk := payload[i*segSize : end]
		id := segments.MessageID(i)
		fp.SetBehavior(id, fakepool.SegmentBehavior{Bytes: chunk})
		segs = append(segs, &metapb.SegmentData{
			Id:          id,
			SegmentSize: int64(len(chunk)),
			StartOffset: 0,
			EndOffset:   int64(len(chunk) - 1),
		})
	}
	return segs
}

// mvfWithCipher is mvfFromMeta plus a real AES cipher, needed by any file whose
// metadata (or nested source) carries an AES key.
func mvfWithCipher(t testing.TB, ctx context.Context, fp *fakepool.Client, name string, m *metapb.FileMetadata) *MetadataVirtualFile {
	t.Helper()
	mvf := mvfFromMeta(t, ctx, fp, name, m)
	mvf.aesCipher = aescipher.NewAesCipher()
	return mvf
}

func TestMigration_AESEncryptedFileStillDecryptsAfterMigration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()
	ms := metadata.NewMetadataService(root)

	// 256 bytes of plaintext, encrypted, served as two 128-byte segments.
	plaintext := plainPattern(256)
	fp := fakepool.New()
	segs := serveBytes(fp, encryptCBC(t, plaintext), 128)
	require.Len(t, segs, 2)

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

	preMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	pre := streamAll(t, mvfWithCipher(t, ctx, fp, "aes-pre", preMeta), preMeta.FileSize)
	require.Equal(t, plaintext, pre, "pre-migration AES file must decrypt to the plaintext")

	_ = runMigration(t, ctx, ms, configDir)

	postMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.NotEmpty(t, postMeta.StoreRef)
	// The key material must survive the rewrite untouched.
	assert.Equal(t, testAESKey, postMeta.AesKey, "aes_key must survive migration")
	assert.Equal(t, testAESIV, postMeta.AesIv, "aes_iv must survive migration")
	assert.Equal(t, metapb.Encryption_AES, postMeta.Encryption)

	post := streamAll(t, mvfWithCipher(t, ctx, fp, "aes-post", postMeta), postMeta.FileSize)
	assert.Equal(t, pre, post, "AES file must stream identically after migration")
	assert.Equal(t, plaintext, post, "AES file must still decrypt to the plaintext")
}

func TestMigration_NestedSharedOuterSourcesStillStreams(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()
	ms := metadata.NewMetadataService(root)

	// One outer "RAR volume" of 512 bytes served as four 128-byte segments. The
	// virtual file is two extents of that volume, both reading through a single
	// shared_outer_sources entry — the layout the Blu-ray dedup produces.
	volume := plainPattern(512)
	fp := fakepool.New()
	outerSegs := serveBytes(fp, volume, 128)
	require.Len(t, outerSegs, 4)

	// Extent 1: volume[64:192]. Extent 2: volume[320:448]. Concatenated = file.
	expected := append(append([]byte{}, volume[64:192]...), volume[320:448]...)

	vpath := filepath.Join("movies", "Nested.m2ts")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize:      int64(len(expected)),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: filepath.Join(configDir, "nested.nzb"),
		SharedOuterSources: []*metapb.NestedSegmentSource{
			{Segments: outerSegs, InnerVolumeSize: int64(len(volume))},
		},
		NestedSources: []*metapb.NestedSegmentSource{
			{SharedOuterSourceIndex: 1, InnerOffset: 64, InnerLength: 128},
			{SharedOuterSourceIndex: 1, InnerOffset: 320, InnerLength: 128},
		},
	}))

	preMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	// The read path expects nested sources hydrated; production does this too.
	require.NoError(t, metadata.ExpandSharedOuterSources(preMeta))
	pre := streamAll(t, mvfFromMeta(t, ctx, fp, "nested-pre", preMeta), preMeta.FileSize)
	require.Equal(t, expected, pre, "pre-migration nested file must stream both extents")

	_ = runMigration(t, ctx, ms, configDir)

	postMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.NotEmpty(t, postMeta.StoreRef)

	// Migration dissolves the dedup: each nested source is self-contained, and
	// shared_outer_sources is gone from disk.
	assert.Empty(t, postMeta.SharedOuterSources, "the dedup must be dissolved by migration")
	require.Len(t, postMeta.NestedSources, 2)
	for i, ns := range postMeta.NestedSources {
		assert.Zero(t, ns.SharedOuterSourceIndex, "nested source %d must no longer reference a shared entry", i)
		assert.Len(t, ns.Segments, len(outerSegs), "nested source %d must carry its own resolved segments", i)
	}

	post := streamAll(t, mvfFromMeta(t, ctx, fp, "nested-post", postMeta), postMeta.FileSize)
	assert.Equal(t, pre, post, "nested file must stream identically after migration")
	assert.Equal(t, expected, post)
}

func TestMigration_NestedEncryptedSourceStillDecryptsAfterMigration(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	configDir := t.TempDir()
	ms := metadata.NewMetadataService(root)

	// An encrypted inner volume: 512 bytes of plaintext, AES-CBC encrypted and
	// served as four 128-byte segments. The file is one extent, volume[128:384].
	volumePlain := plainPattern(512)
	fp := fakepool.New()
	outerSegs := serveBytes(fp, encryptCBC(t, volumePlain), 128)
	require.Len(t, outerSegs, 4)

	expected := volumePlain[128:384]

	vpath := filepath.Join("movies", "NestedEnc.mkv")
	require.NoError(t, ms.WriteFileMetadata(vpath, &metapb.FileMetadata{
		FileSize:      int64(len(expected)),
		Status:        metapb.FileStatus_FILE_STATUS_HEALTHY,
		SourceNzbPath: filepath.Join(configDir, "nestedenc.nzb"),
		NestedSources: []*metapb.NestedSegmentSource{
			{
				Segments:        outerSegs,
				AesKey:          testAESKey,
				AesIv:           testAESIV,
				InnerOffset:     128,
				InnerLength:     int64(len(expected)),
				InnerVolumeSize: int64(len(volumePlain)),
			},
		},
	}))

	preMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	pre := streamAll(t, mvfWithCipher(t, ctx, fp, "nestedenc-pre", preMeta), preMeta.FileSize)
	require.Equal(t, expected, pre, "pre-migration encrypted nested source must decrypt")

	_ = runMigration(t, ctx, ms, configDir)

	postMeta, err := ms.ReadFileMetadata(vpath)
	require.NoError(t, err)
	require.NotEmpty(t, postMeta.StoreRef)
	require.Len(t, postMeta.NestedSources, 1)

	ns := postMeta.NestedSources[0]
	assert.Equal(t, testAESKey, ns.AesKey, "nested aes_key must survive migration")
	assert.Equal(t, testAESIV, ns.AesIv, "nested aes_iv must survive migration")
	assert.EqualValues(t, len(volumePlain), ns.InnerVolumeSize, "inner_volume_size must survive migration")
	assert.EqualValues(t, 128, ns.InnerOffset)
	assert.EqualValues(t, len(expected), ns.InnerLength)

	post := streamAll(t, mvfWithCipher(t, ctx, fp, "nestedenc-post", postMeta), postMeta.FileSize)
	assert.Equal(t, pre, post, "encrypted nested source must stream identically after migration")
	assert.Equal(t, expected, post)
	assert.False(t, bytes.Equal(post, encryptCBC(t, volumePlain)[128:384]),
		"sanity: the stream must be plaintext, not ciphertext")
}
