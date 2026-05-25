// Package repair drives PAR2-backed self-healing of corrupt Usenet files.
// It ties the format-agnostic reconstruction engine (internal/repair/par2) to
// AltMount's metadata, NNTP pool, and segment cache: given a virtual file that
// failed to stream, it fetches the file's PAR2 recovery data, rebuilds the
// missing segments, and writes their decoded bytes into the segment cache so
// subsequent reads serve them transparently — avoiding a full re-download.
package repair

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"

	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/repair/par2"
)

var (
	// ErrNoMetadata is returned when the virtual file has no metadata.
	ErrNoMetadata = errors.New("repair: file metadata not found")
	// ErrNoPar2 is returned when the file carries no PAR2 recovery data, so
	// segment-level repair is impossible and the caller should fall back to a
	// full re-download (ARR rescan).
	ErrNoPar2 = errors.New("repair: file has no PAR2 recovery data")
	// ErrNoSegments is returned when the file has no segment layout.
	ErrNoSegments = errors.New("repair: file has no segments")
	// ErrPar2Mismatch is returned when the PAR2 recovery set does not describe
	// this exact file (e.g. it protects the surrounding RAR volumes rather than
	// the decoded stream). Segment offsets would not line up with PAR2 slices,
	// so we refuse rather than reconstruct garbage.
	ErrPar2Mismatch = errors.New("repair: PAR2 recovery set does not match this file")
)

// MetadataReader reads the full proto metadata (segments + PAR2 references) for
// a virtual file.
type MetadataReader interface {
	ReadFileMetadata(virtualPath string) (*metapb.FileMetadata, error)
}

// ArticleFetcher fetches one decoded article body by message-ID. A return of
// (nil, true, nil) means the article is permanently missing from all providers;
// any other error is transient/operational.
type ArticleFetcher interface {
	Fetch(ctx context.Context, messageID string) (data []byte, missing bool, err error)
}

// Sink stores reconstructed decoded segment bytes by message-ID. The segment
// cache satisfies this directly.
type Sink interface {
	Put(messageID string, data []byte) error
}

// SlotAcquirer optionally budgets a repair against the NNTP connection
// admission gate so self-healing does not starve live streams. May be nil.
type SlotAcquirer interface {
	AcquireImportSlot(ctx context.Context) (release func(), err error)
}

// Service performs PAR2 self-healing for individual virtual files.
type Service struct {
	meta  MetadataReader
	fetch ArticleFetcher
	sink  Sink
	slots SlotAcquirer
	log   *slog.Logger
}

// NewService constructs a repair Service. slots may be nil to skip admission
// budgeting; log defaults to slog.Default() if nil.
func NewService(meta MetadataReader, fetch ArticleFetcher, sink Sink, slots SlotAcquirer, log *slog.Logger) *Service {
	if log == nil {
		log = slog.Default()
	}
	return &Service{meta: meta, fetch: fetch, sink: sink, slots: slots, log: log.With("component", "par2-repair")}
}

// Outcome reports the result of a successful repair.
type Outcome struct {
	RecoveredSegments []string // message-IDs reconstructed and written to the sink
	SlicesFixed       int      // PAR2 input slices reconstructed
	MissingSegments   int      // segments found missing before repair
}

// RepairFile attempts to reconstruct any missing segments of virtualPath from
// its PAR2 recovery data. On success the recovered segments are written to the
// sink. It returns an error suitable for fallback decisions (ErrNoPar2,
// ErrPar2Mismatch, par2.ErrInsufficientRecovery, par2.ErrMultiFileUnsupported,
// or a transient fetch error).
func (s *Service) RepairFile(ctx context.Context, virtualPath string) (*Outcome, error) {
	meta, err := s.meta.ReadFileMetadata(virtualPath)
	if err != nil {
		return nil, fmt.Errorf("repair: read metadata: %w", err)
	}
	if meta == nil {
		return nil, ErrNoMetadata
	}
	if len(meta.GetPar2Files()) == 0 {
		return nil, ErrNoPar2
	}
	segs := buildSegments(meta.GetSegmentData())
	if len(segs) == 0 {
		return nil, ErrNoSegments
	}

	if s.slots != nil {
		release, err := s.slots.AcquireImportSlot(ctx)
		if err != nil {
			return nil, fmt.Errorf("repair: acquire admission slot: %w", err)
		}
		defer release()
	}

	// 1. Fetch + parse the PAR2 recovery data.
	rs, err := s.loadRecoverySet(ctx, meta.GetPar2Files())
	if err != nil {
		return nil, err
	}

	// 2. Safety: the recovery set must describe exactly this file. A single
	// recovery file whose length equals ours means the PAR2 protects our
	// decoded stream directly (segment offsets line up with PAR2 slices). A
	// multi-file set or a length mismatch (e.g. PAR2 over the RAR volumes) is
	// not reconstructable from this file's metadata alone.
	if len(rs.RecoveryFileIDs) != 1 {
		return nil, par2.ErrMultiFileUnsupported
	}
	fd := rs.Files[rs.RecoveryFileIDs[0]]
	if fd == nil || int64(fd.Length) != meta.GetFileSize() {
		return nil, ErrPar2Mismatch
	}

	// 3. Probe every segment once, classifying present vs. permanently missing
	// and caching present (decoded) bytes for reconstruction. This is the
	// whole-file read that Reed-Solomon recovery inherently requires.
	cache := make(map[string][]byte, len(segs))
	missing := make(map[string]bool)
	for _, seg := range segs {
		data, miss, err := s.fetch.Fetch(ctx, seg.MessageID)
		if err != nil {
			return nil, fmt.Errorf("repair: fetch segment %s: %w", seg.MessageID, err)
		}
		if miss {
			missing[seg.MessageID] = true
			continue
		}
		cache[seg.MessageID] = data
	}
	if len(missing) == 0 {
		// Nothing actually missing — the original failure may have been
		// transient. Report a no-op so the caller can simply retry the read.
		return &Outcome{}, nil
	}

	// 4. Reconstruct and sink.
	res, err := par2.RepairFileSegments(ctx, rs, segs, missing, cachedFetcher{cache}, s.sink)
	if err != nil {
		return nil, err
	}
	s.log.InfoContext(ctx, "PAR2 self-heal succeeded",
		"file", virtualPath,
		"missing_segments", len(missing),
		"recovered_segments", len(res.Recovered),
		"slices_fixed", res.SlicesFixed)
	return &Outcome{
		RecoveredSegments: res.Recovered,
		SlicesFixed:       res.SlicesFixed,
		MissingSegments:   len(missing),
	}, nil
}

// loadRecoverySet fetches the Usenet segments backing each PAR2 file, in offset
// order, and parses the concatenation. PAR2 articles that are themselves
// missing are skipped; the parser resynchronises on packet boundaries, so a
// hole only costs the packets that span it.
func (s *Service) loadRecoverySet(ctx context.Context, par2Files []*metapb.Par2FileReference) (*par2.RecoverySet, error) {
	var buf []byte
	for _, pf := range par2Files {
		segs := append([]*metapb.SegmentData(nil), pf.GetSegmentData()...)
		sort.Slice(segs, func(i, j int) bool { return segs[i].GetStartOffset() < segs[j].GetStartOffset() })
		for _, seg := range segs {
			data, miss, err := s.fetch.Fetch(ctx, seg.GetId())
			if err != nil {
				return nil, fmt.Errorf("repair: fetch PAR2 segment %s: %w", seg.GetId(), err)
			}
			if miss {
				s.log.DebugContext(ctx, "PAR2 segment missing, skipping", "segment_id", seg.GetId())
				continue
			}
			buf = append(buf, data...)
		}
	}
	if len(buf) == 0 {
		return nil, ErrNoPar2
	}
	return par2.Parse(buf)
}

// buildSegments converts proto segment data into the engine's segment layout,
// ordered by start offset. AltMount stores EndOffset as the inclusive index of
// the last byte (segSize == EndOffset - StartOffset + 1), whereas the engine
// uses a half-open [Start, End) range, hence the +1.
func buildSegments(in []*metapb.SegmentData) []par2.Segment {
	out := make([]par2.Segment, 0, len(in))
	for _, s := range in {
		out = append(out, par2.Segment{
			MessageID: s.GetId(),
			Start:     s.GetStartOffset(),
			End:       s.GetEndOffset() + 1,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Start < out[j].Start })
	return out
}

// cachedFetcher serves the already-fetched present segments to the engine.
type cachedFetcher struct{ m map[string][]byte }

func (c cachedFetcher) Fetch(_ context.Context, messageID string) ([]byte, error) {
	d, ok := c.m[messageID]
	if !ok {
		return nil, fmt.Errorf("repair: present segment %s not cached", messageID)
	}
	return d, nil
}
