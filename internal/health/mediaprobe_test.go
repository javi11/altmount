package health

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/mediaprobe"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/testsupport/fakepool"
	"github.com/javi11/nntppool/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakePoolManager is a pool.Manager backed by a fakepool.Client, so health
// checks can stat and download segments without a network.
type fakePoolManager struct {
	mockPoolManager
	client *fakepool.Client
}

func (m *fakePoolManager) GetPool() (pool.NntpClient, error) { return m.client, nil }
func (m *fakePoolManager) HasPool() bool                     { return true }

// --- minimal MP4 fixture (mirrors internal/mediaprobe test builders) ---

func testMP4Box(fourcc string, payload ...[]byte) []byte {
	var body bytes.Buffer
	for _, p := range payload {
		body.Write(p)
	}
	out := make([]byte, 8+body.Len())
	binary.BigEndian.PutUint32(out[:4], uint32(8+body.Len()))
	copy(out[4:8], fourcc)
	copy(out[8:], body.Bytes())
	return out
}

func testMP4File(mdatSize int) []byte {
	mvhd := make([]byte, 100)
	binary.BigEndian.PutUint32(mvhd[12:16], 1000)      // timescale
	binary.BigEndian.PutUint32(mvhd[16:20], 3_600_000) // duration: 1h
	var f []byte
	f = append(f, testMP4Box("ftyp", []byte("isomiso2"))...)
	f = append(f, testMP4Box("moov", testMP4Box("mvhd", mvhd))...)
	f = append(f, testMP4Box("mdat", make([]byte, mdatSize))...)
	return f
}

// probeTestEnv wires a HealthChecker over a fake pool that serves an MP4 file
// split into fixed-size segments.
type probeTestEnv struct {
	checker  *HealthChecker
	ms       *metadata.MetadataService
	fp       *fakepool.Client
	filePath string
	file     []byte
	segSize  int64
	segIDs   []string
	cfg      *config.Config
}

func newProbeTestEnv(t *testing.T, fileName string, file []byte, segSize int64) *probeTestEnv {
	t.Helper()
	tempDir := t.TempDir()
	ms := metadata.NewMetadataService(tempDir)
	fp := fakepool.New()

	var segs []*metapb.SegmentData
	var ids []string
	for off := int64(0); off < int64(len(file)); off += segSize {
		end := min(off+segSize, int64(len(file)))
		id := fmt.Sprintf("probe-seg-%d@test", len(segs))
		fp.SetBehavior(id, fakepool.SegmentBehavior{Bytes: file[off:end]})
		segs = append(segs, &metapb.SegmentData{
			Id:          id,
			SegmentSize: end - off,
			StartOffset: 0,
			EndOffset:   end - off - 1,
		})
		ids = append(ids, id)
	}

	filePath := "/movies/" + fileName
	meta := ms.CreateFileMetadata(
		int64(len(file)), "test.nzb", metapb.FileStatus_FILE_STATUS_HEALTHY,
		segs, metapb.Encryption_NONE, "", "", nil, nil, 0, nil, "",
	)
	require.NoError(t, ms.WriteFileMetadata(filePath, meta))

	healthEnabled := true
	cfg := config.DefaultConfig()
	cfg.Health.Enabled = &healthEnabled
	cfg.Metadata.RootPath = tempDir
	cfg.Health.MaxConnectionsForHealthChecks = 2
	checkAll := true
	cfg.Health.CheckAllSegments = &checkAll // deterministic: stat every segment

	checker := NewHealthChecker(
		nil, // healthRepo unused by CheckFile happy paths (no deletes)
		ms,
		&fakePoolManager{client: fp},
		func() *config.Config { return cfg },
		nil,
	)

	return &probeTestEnv{
		checker:  checker,
		ms:       ms,
		fp:       fp,
		filePath: filePath,
		file:     file,
		segSize:  segSize,
		segIDs:   ids,
		cfg:      cfg,
	}
}

// markSegmentMissing makes both Stat and Body fail for the segment.
func (e *probeTestEnv) markSegmentMissing(index int) {
	e.fp.SetBehavior(e.segIDs[index], fakepool.SegmentBehavior{Err: nntppool.ErrArticleNotFound})
}

func TestHealthCheckPersistsMediaStructureOnFirstCheck(t *testing.T) {
	env := newProbeTestEnv(t, "movie.mp4", testMP4File(8192), 1024)

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type, "err: %v", event.Error)

	meta, err := env.ms.ReadFileMetadata(env.filePath)
	require.NoError(t, err)
	require.NotNil(t, meta.MediaStructure, "structure should be persisted on first healthy check")
	assert.Equal(t, "mp4", meta.MediaStructure.Container)
	assert.InDelta(t, 3600.0, meta.MediaStructure.DurationSeconds, 0.01)
	assert.NotEmpty(t, meta.MediaStructure.Critical)
	assert.NotEmpty(t, meta.MediaStructure.Payload)

	// Second check must not probe again: body-call count stays flat.
	bodyCallsAfterFirst := env.fp.BodyPriorityCalls()
	event = env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type)
	assert.Equal(t, bodyCallsAfterFirst, env.fp.BodyPriorityCalls(),
		"second healthy check must not re-probe")
}

func TestHealthCheckProbeDisabledByConfig(t *testing.T) {
	env := newProbeTestEnv(t, "movie.mp4", testMP4File(8192), 1024)
	probeOff := false
	env.cfg.Health.MediaProbeEnabled = &probeOff

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type)

	meta, err := env.ms.ReadFileMetadata(env.filePath)
	require.NoError(t, err)
	assert.Nil(t, meta.MediaStructure, "probe disabled: no structure persisted")
	assert.Equal(t, int64(0), env.fp.BodyPriorityCalls(), "probe disabled: no downloads")
}

func TestHealthCheckSkipsProbeForNonVideo(t *testing.T) {
	env := newProbeTestEnv(t, "archive.rar", testMP4File(4096), 1024)

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type)

	meta, err := env.ms.ReadFileMetadata(env.filePath)
	require.NoError(t, err)
	assert.Nil(t, meta.MediaStructure)
}

func TestHealthCheckClassifiesMissingPayloadAsDegraded(t *testing.T) {
	file := testMP4File(16 * 1024)
	env := newProbeTestEnv(t, "movie.mp4", file, 1024)
	// Segment 10 sits deep inside mdat (boxes before mdat are < 200 bytes).
	env.markSegmentMissing(10)

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileCorrupted, event.Type)
	require.NotNil(t, event.Classification)
	assert.Equal(t, mediaprobe.VerdictDegraded, event.Classification.Verdict, "reason: %s", event.Classification.Reason)

	// Details envelope round-trips with playback impact.
	require.NotNil(t, event.Details)
	var details database.HealthErrorDetails
	require.NoError(t, json.Unmarshal([]byte(*event.Details), &details))
	assert.Equal(t, "missing_segments", details.ErrorType)
	assert.Equal(t, 1, details.MissingArticles)
	require.NotNil(t, details.PlaybackImpact)
	assert.Equal(t, mediaprobe.VerdictDegraded, details.PlaybackImpact.Verdict)
	assert.NotEmpty(t, details.PlaybackImpact.MissingRanges)

	// The lazy probe persists the structure for future offline checks.
	meta, err := env.ms.ReadFileMetadata(env.filePath)
	require.NoError(t, err)
	assert.NotNil(t, meta.MediaStructure)
}

func TestHealthCheckClassifiesMissingHeaderAsFatalOrUnknown(t *testing.T) {
	file := testMP4File(16 * 1024)
	env := newProbeTestEnv(t, "movie.mp4", file, 1024)
	// Segment 0 contains ftyp+moov: the live probe cannot even read the
	// header (missing-aware reader), so the verdict is unknown → treated
	// as fatal by callers.
	env.markSegmentMissing(0)

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileCorrupted, event.Type)
	require.NotNil(t, event.Classification)
	assert.Equal(t, mediaprobe.VerdictUnknown, event.Classification.Verdict)
}

func TestHealthCheckUsesStoredStructureWithoutNetwork(t *testing.T) {
	file := testMP4File(16 * 1024)
	env := newProbeTestEnv(t, "movie.mp4", file, 1024)

	// First: healthy check probes and persists the structure.
	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type)
	probeBodyCalls := env.fp.BodyPriorityCalls()

	// Now the moov segment dies. The stored structure classifies it as
	// fatal offline — impossible for the live probe (which would return
	// unknown because it cannot read moov).
	env.markSegmentMissing(0)
	event = env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileCorrupted, event.Type)
	require.NotNil(t, event.Classification)
	assert.Equal(t, mediaprobe.VerdictFatal, event.Classification.Verdict, "reason: %s", event.Classification.Reason)
	assert.Equal(t, probeBodyCalls, env.fp.BodyPriorityCalls(),
		"stored-structure classification must not download anything")
}

func TestHealthCheckSkipsProbeForEncryptedFiles(t *testing.T) {
	env := newProbeTestEnv(t, "movie.mp4", testMP4File(4096), 1024)

	// Flip the metadata to an encrypted variant.
	require.NoError(t, env.ms.UpdateFileMetadata(env.filePath, func(m *metapb.FileMetadata) {
		m.Encryption = metapb.Encryption_RCLONE
	}))

	event := env.checker.CheckFile(context.Background(), env.filePath)
	require.Equal(t, EventTypeFileHealthy, event.Type)

	meta, err := env.ms.ReadFileMetadata(env.filePath)
	require.NoError(t, err)
	assert.Nil(t, meta.MediaStructure, "encrypted file must not be probed")
}
