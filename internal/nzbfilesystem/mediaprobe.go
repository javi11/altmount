package nzbfilesystem

import (
	"context"
	"io"
	"log/slog"
	"time"

	"github.com/javi11/altmount/internal/mediaprobe"
	"github.com/javi11/altmount/internal/metadata"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/usenet"
)

// streamingProbeTimeout bounds the lazy container probe run on a streaming
// failure. The fast path (structure persisted at import time) does no I/O.
const streamingProbeTimeout = 10 * time.Second

// streamMissingAwareReaderAt mirrors the health checker's wrapper: probe reads
// that touch the known-missing range fail immediately with ErrMissingRange
// instead of hitting dead articles.
type streamMissingAwareReaderAt struct {
	inner   io.ReaderAt
	missing []mediaprobe.ByteRange
}

func (r *streamMissingAwareReaderAt) ReadAt(p []byte, off int64) (int, error) {
	req := mediaprobe.ByteRange{Start: off, End: off + int64(len(p)) - 1}
	for _, m := range r.missing {
		if req.Start <= m.End && m.Start <= req.End {
			return 0, mediaprobe.ErrMissingRange
		}
	}
	return r.inner.ReadAt(p, off)
}

// classifyStreamingFailure determines whether the segment that failed during
// streaming breaks playback. Returns nil when classification is not possible
// (probe disabled, non-video, encrypted/nested/remuxed, or no failure offset).
func (mvf *MetadataVirtualFile) classifyStreamingFailure(dcErr *usenet.DataCorruptionError) *mediaprobe.Classification {
	cfg := mvf.configGetter()
	if !cfg.GetMediaProbeEnabled() {
		return nil
	}
	if mediaprobe.ContainerForFile(mvf.name) == "" ||
		mvf.meta.Encryption != metapb.Encryption_NONE ||
		len(mvf.meta.NestedSources) > 0 ||
		len(mvf.meta.ClipBoundaries) > 0 {
		return nil
	}
	if dcErr.FileOffset < 0 {
		return nil // failure position unknown (e.g. synthetic errors)
	}

	// Expand the failure offset to the full byte range of its segment.
	idx := buildSegmentIndex(mvf.meta.SegmentData)
	segIdx := idx.findSegmentForOffset(dcErr.FileOffset)
	if segIdx < 0 {
		return nil
	}
	missing := []mediaprobe.ByteRange{{
		Start: idx.offsets[segIdx],
		End:   idx.offsets[segIdx] + idx.sizes[segIdx] - 1,
	}}

	// Fast path: structure persisted at import time — pure intersection.
	if s := metadata.MediaStructureFromProto(mvf.meta.MediaStructure, mvf.meta.FileSize); s != nil {
		cls := mediaprobe.ClassifyAgainst(s, missing)
		return &cls
	}

	// Lazy fallback: live probe over still-available segments. Runs at most
	// once per repair-coalescer debounce window (the caller already passed
	// ShouldTrigger). The structure is deliberately not persisted here: the
	// metadata file may be moved to the safety folder moments later.
	if mvf.poolManager == nil {
		return nil
	}
	probeCtx, cancel := context.WithTimeout(mvf.ctx, streamingProbeTimeout)
	defer cancel()
	reader := &streamMissingAwareReaderAt{
		inner: usenet.NewSegmentsReaderAt(probeCtx,
			newMetadataSegmentLoader(mvf.meta.SegmentData),
			mvf.poolManager.GetPool, mvf.poolManager, mvf.meta.FileSize),
		missing: missing,
	}
	cls := mediaprobe.Classify(probeCtx, reader, mvf.meta.FileSize, mvf.name, missing)
	slog.DebugContext(mvf.ctx, "Classified streaming failure",
		"file", mvf.name,
		"verdict", cls.Verdict,
		"reason", cls.Reason)
	return &cls
}
