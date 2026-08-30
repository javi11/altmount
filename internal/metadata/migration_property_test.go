package metadata

import (
	"context"
	"fmt"
	"hash/fnv"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

// migration_property_test.go answers "are all cases covered?" by not enumerating
// cases at all. It generates randomized legacy metadata across every
// segment-bearing and preserved field — partial offsets, PAR2 sets, nested
// sources with and without the shared-outer dedup, all four encryption modes,
// clip boundaries, known holes, nzbdav ids, duplicate and shared segment ids,
// multi-file groups, empty files — migrates it, and asserts the invariant.
//
// The assertions here are deliberately independent of verifyMigratedFile: if the
// migration's own checker had a blind spot, comparing against it would inherit
// that blind spot. The expected values are recomputed from a pristine clone.

const idPoolSize = 24

// idPool returns message ids drawn from a small pool so that generated files
// collide with each other, exercising cross-file dedup and the non-increasing
// store-index path in splitRefs.
func idPool() []string {
	pool := make([]string, idPoolSize)
	for i := range pool {
		pool[i] = fmt.Sprintf("prop-%03d@news.example.com", i)
	}
	return pool
}

func randSegments(rng *rand.Rand, pool []string, n int) []*metapb.SegmentData {
	if n == 0 {
		return nil
	}
	segs := make([]*metapb.SegmentData, n)
	for i := range segs {
		size := int64(1 + rng.Intn(1000))
		start, end := int64(0), size-1
		// Sometimes slice the segment the way an archive extent does.
		if rng.Intn(3) == 0 && size > 2 {
			start = int64(rng.Intn(int(size - 1)))
			end = start + int64(rng.Intn(int(size-start)))
		}
		segs[i] = &metapb.SegmentData{
			Id:          pool[rng.Intn(len(pool))],
			SegmentSize: size,
			StartOffset: start,
			EndOffset:   end,
		}
	}
	return segs
}

func randNestedSource(rng *rand.Rand, pool []string) *metapb.NestedSegmentSource {
	ns := &metapb.NestedSegmentSource{
		Segments:        randSegments(rng, pool, 1+rng.Intn(4)),
		InnerOffset:     int64(rng.Intn(5000)),
		InnerLength:     int64(1 + rng.Intn(5000)),
		InnerVolumeSize: int64(1 + rng.Intn(100000)),
	}
	if rng.Intn(2) == 0 {
		ns.AesKey = []byte("0123456789abcdef")
		ns.AesIv = []byte("fedcba9876543210")
	}
	return ns
}

// randomLegacyMeta builds a v1 FileMetadata exercising a random combination of
// every field the migration touches or must preserve.
func randomLegacyMeta(rng *rand.Rand, pool []string) *metapb.FileMetadata {
	m := &metapb.FileMetadata{
		FileSize:    int64(rng.Intn(1 << 30)),
		Status:      []metapb.FileStatus{metapb.FileStatus_FILE_STATUS_HEALTHY, metapb.FileStatus_FILE_STATUS_CORRUPTED, metapb.FileStatus_FILE_STATUS_DEGRADED}[rng.Intn(3)],
		CreatedAt:   int64(rng.Intn(1 << 31)),
		ModifiedAt:  int64(rng.Intn(1 << 31)),
		ReleaseDate: int64(rng.Intn(1 << 31)),
		SegmentData: randSegments(rng, pool, rng.Intn(12)),
	}

	switch rng.Intn(4) {
	case 0: // NONE
	case 1: // RCLONE — password/salt must survive
		m.Encryption = metapb.Encryption_RCLONE
		m.Password = fmt.Sprintf("pw-%d", rng.Int())
		m.Salt = fmt.Sprintf("salt-%d", rng.Int())
	case 2: // HEADERS
		m.Encryption = metapb.Encryption_HEADERS
	case 3: // AES — key material must survive
		m.Encryption = metapb.Encryption_AES
		m.AesKey = []byte("0123456789abcdef")
		m.AesIv = []byte("fedcba9876543210")
	}

	for i := 0; i < rng.Intn(3); i++ {
		m.Par2Files = append(m.Par2Files, &metapb.Par2FileReference{
			Filename:    fmt.Sprintf("rel.vol%02d.par2", i),
			FileSize:    int64(rng.Intn(1 << 20)),
			SegmentData: randSegments(rng, pool, 1+rng.Intn(4)),
		})
	}

	// Nested sources: either self-contained, or sharing outer sources through
	// the dedup that migration dissolves.
	switch nested := rng.Intn(3); nested {
	case 1:
		for i := 0; i < 1+rng.Intn(3); i++ {
			m.NestedSources = append(m.NestedSources, randNestedSource(rng, pool))
		}
	case 2:
		shared := 1 + rng.Intn(2)
		for i := 0; i < shared; i++ {
			m.SharedOuterSources = append(m.SharedOuterSources, randNestedSource(rng, pool))
		}
		for i := 0; i < 1+rng.Intn(4); i++ {
			m.NestedSources = append(m.NestedSources, &metapb.NestedSegmentSource{
				SharedOuterSourceIndex: int32(1 + rng.Intn(shared)),
				InnerOffset:            int64(rng.Intn(5000)),
				InnerLength:            int64(1 + rng.Intn(5000)),
			})
		}
	}

	for i := 0; i < rng.Intn(3); i++ {
		m.ClipBoundaries = append(m.ClipBoundaries, &metapb.ClipBoundary{
			ByteLen:   int64(1 + rng.Intn(1<<20)),
			Delta_90K: int64(rng.Intn(1 << 20)),
		})
	}
	for i := 0; i < rng.Intn(2); i++ {
		m.KnownHoles = append(m.KnownHoles, &metapb.HoleRun{
			StartSegment: int64(rng.Intn(100)),
			Count:        int64(1 + rng.Intn(5)),
		})
	}
	if rng.Intn(3) == 0 {
		m.NzbdavId = fmt.Sprintf("nzbdav-%d", rng.Int())
	}
	return m
}

// assertPreserved compares a migrated file against the pristine expectation,
// recomputed independently of the migration's own verifier.
func assertPreserved(t *testing.T, label string, want, got *metapb.FileMetadata) {
	t.Helper()

	require.NoError(t, sameResolvedSegments(label+" main", want.SegmentData, got.SegmentData))

	require.Len(t, got.Par2Files, len(want.Par2Files), "%s par2 count", label)
	for i := range want.Par2Files {
		require.NoError(t, sameResolvedSegments(
			fmt.Sprintf("%s par2[%d]", label, i),
			want.Par2Files[i].SegmentData, got.Par2Files[i].SegmentData))
		assert.Equal(t, want.Par2Files[i].Filename, got.Par2Files[i].Filename)
		assert.Equal(t, want.Par2Files[i].FileSize, got.Par2Files[i].FileSize)
	}

	require.Len(t, got.NestedSources, len(want.NestedSources), "%s nested count", label)
	for i := range want.NestedSources {
		w, g := want.NestedSources[i], got.NestedSources[i]
		require.NoError(t, sameResolvedSegments(
			fmt.Sprintf("%s nested[%d]", label, i), w.Segments, g.Segments))
		assert.Equal(t, w.InnerOffset, g.InnerOffset, "%s nested[%d] inner offset", label, i)
		assert.Equal(t, w.InnerLength, g.InnerLength, "%s nested[%d] inner length", label, i)
		assert.Equal(t, w.InnerVolumeSize, g.InnerVolumeSize, "%s nested[%d] inner volume size", label, i)
		assert.Equal(t, w.AesKey, g.AesKey, "%s nested[%d] aes key", label, i)
		assert.Equal(t, w.AesIv, g.AesIv, "%s nested[%d] aes iv", label, i)
	}

	assert.Equal(t, want.FileSize, got.FileSize, "%s file size", label)
	assert.Equal(t, want.Status, got.Status, "%s status", label)
	assert.Equal(t, want.Encryption, got.Encryption, "%s encryption", label)
	assert.Equal(t, want.Password, got.Password, "%s rclone password", label)
	assert.Equal(t, want.Salt, got.Salt, "%s rclone salt", label)
	assert.Equal(t, want.AesKey, got.AesKey, "%s aes key", label)
	assert.Equal(t, want.AesIv, got.AesIv, "%s aes iv", label)
	assert.Equal(t, want.ReleaseDate, got.ReleaseDate, "%s release date", label)
	assert.Equal(t, want.NzbdavId, got.NzbdavId, "%s nzbdav id (.id sidecar)", label)

	require.Len(t, got.ClipBoundaries, len(want.ClipBoundaries), "%s clip boundary count", label)
	for i := range want.ClipBoundaries {
		assert.Equal(t, want.ClipBoundaries[i].ByteLen, got.ClipBoundaries[i].ByteLen)
		assert.Equal(t, want.ClipBoundaries[i].Delta_90K, got.ClipBoundaries[i].Delta_90K)
	}
	require.Len(t, got.KnownHoles, len(want.KnownHoles), "%s known hole count", label)
	for i := range want.KnownHoles {
		assert.Equal(t, want.KnownHoles[i].StartSegment, got.KnownHoles[i].StartSegment)
		assert.Equal(t, want.KnownHoles[i].Count, got.KnownHoles[i].Count)
	}
}

// migrateRandomGroup generates a group of random legacy files, migrates it, and
// asserts every file survived unchanged. Returns the number of files generated.
func migrateRandomGroup(t *testing.T, rng *rand.Rand) int {
	t.Helper()
	root := t.TempDir()
	storeDir := t.TempDir()
	ms := NewMetadataService(root)
	pool := idPool()

	fileCount := 1 + rng.Intn(4)
	paths := make([]string, fileCount)
	expected := make([]*metapb.FileMetadata, fileCount)

	for i := 0; i < fileCount; i++ {
		m := randomLegacyMeta(rng, pool)
		m.SourceNzbPath = "/nzbs/prop.nzb"
		paths[i] = filepath.Join("movies", fmt.Sprintf("F%02d.mkv", i))
		require.NoError(t, ms.WriteFileMetadata(paths[i], m))

		// nzbdav ids live in a .id sidecar written outside this package (an
		// imported nzbdav library); WriteFileMetadata deliberately never
		// persists the proto field. Create the sidecar the way production does
		// so the assertion below tests the real invariant: migration must not
		// disturb it.
		if m.NzbdavId != "" {
			require.NoError(t, os.WriteFile(
				filepath.Join(root, paths[i]+".meta.id"), []byte(m.NzbdavId), 0644))
		}

		// Expectation: the pristine shape with the dedup expanded, since
		// migration legitimately dissolves shared_outer_sources.
		want := proto.Clone(m).(*metapb.FileMetadata)
		require.NoError(t, ExpandSharedOuterSources(want))
		expected[i] = want
	}

	groups, err := ms.ScanLegacyMetas()
	require.NoError(t, err)
	require.Len(t, groups, 1)

	res, err := ms.MigrateGroup(context.Background(), groups[0], storeDir, "alt.binaries.misc")
	require.NoError(t, err)
	require.Equal(t, 0, res.FilesFailed, "no file may fail: %v", res.Failures)
	require.Equal(t, fileCount, res.FilesMigrated)

	for i, p := range paths {
		raw, readErr := os.ReadFile(filepath.Join(root, p+".meta"))
		require.NoError(t, readErr)
		require.True(t, isV3Meta(raw), "%s must be v3 on disk", p)

		got, readErr := ms.ReadFileMetadata(p)
		require.NoError(t, readErr)
		require.NotNil(t, got)
		assertPreserved(t, p, expected[i], got)
	}
	return fileCount
}

func TestMigration_PropertyRandomShapes(t *testing.T) {
	const iterations = 300
	totalFiles := 0
	for i := 0; i < iterations; i++ {
		rng := rand.New(rand.NewSource(int64(i)))
		t.Run(fmt.Sprintf("seed%03d", i), func(t *testing.T) {
			totalFiles += migrateRandomGroup(t, rng)
		})
	}
	t.Logf("migrated %d randomly generated files across %d groups", totalFiles, iterations)
}

// FuzzMigrationPreservesSegments lets the fuzzer explore the shape space beyond
// the fixed seeds. Run longer with:
//
//	go test ./internal/metadata/ -run=Fuzz -fuzz=FuzzMigrationPreservesSegments -fuzztime=5m
func FuzzMigrationPreservesSegments(f *testing.F) {
	f.Add([]byte("seed"))
	f.Add([]byte(""))
	f.Add([]byte{0x00, 0xff, 0x7f})
	f.Add([]byte("archive-nested-encrypted"))

	f.Fuzz(func(t *testing.T, data []byte) {
		h := fnv.New64a()
		_, _ = h.Write(data)
		migrateRandomGroup(t, rand.New(rand.NewSource(int64(h.Sum64()))))
	})
}
