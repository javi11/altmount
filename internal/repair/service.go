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
	// ErrProbeUnsupported is returned by ProbeRepairable when the configured
	// ArticleFetcher cannot perform a cheap existence check (NNTP STAT).
	ErrProbeUnsupported = errors.New("repair: article prober not available")
	// ErrFileTooLarge is returned when a file exceeds the configured self-heal
	// size ceiling. Reconstruction buffers the whole file in RAM, so very large
	// files fall back to ARR re-download instead.
	ErrFileTooLarge = errors.New("repair: file exceeds self-heal size limit")
)

// ArticleProber optionally reports article existence without downloading the
// body (NNTP STAT). When the configured ArticleFetcher also implements this,
// ProbeRepairable can decide whether a file needs — and can be — repaired
// without pulling the whole file over the wire.
type ArticleProber interface {
	Probe(ctx context.Context, messageID string) (exists bool, err error)
}

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

// DefaultMaxConcurrentRepairs bounds in-flight reconstructions when the caller
// does not configure a limit. A reconstruction buffers the whole file in RAM
// (~2× file size: the present-segment map plus the parallel slice array), so an
// unbounded count of concurrent large-file heals could OOM the host.
const DefaultMaxConcurrentRepairs = 1

// Service performs PAR2 self-healing for individual virtual files.
type Service struct {
	meta         MetadataReader
	fetch        ArticleFetcher
	sink         Sink
	slots        SlotAcquirer
	log          *slog.Logger
	sem          chan struct{} // bounds concurrent reconstructions (peak RAM)
	maxFileBytes int64         // skip self-heal above this size; 0 == unlimited
}

// Option customises a Service. Unset options keep the documented defaults.
type Option func(*Service)

// WithMaxConcurrentRepairs caps how many reconstructions may run at once. Values
// < 1 fall back to DefaultMaxConcurrentRepairs.
func WithMaxConcurrentRepairs(n int) Option {
	return func(s *Service) {
		if n < 1 {
			n = DefaultMaxConcurrentRepairs
		}
		s.sem = make(chan struct{}, n)
	}
}

// WithMaxRepairFileBytes refuses self-heal for files larger than maxBytes,
// falling back to ARR so a few multi-GB heals can't exhaust RAM. <= 0 means
// unlimited (the default).
func WithMaxRepairFileBytes(maxBytes int64) Option {
	return func(s *Service) {
		if maxBytes < 0 {
			maxBytes = 0
		}
		s.maxFileBytes = maxBytes
	}
}

// NewService constructs a repair Service. slots may be nil to skip admission
// budgeting; log defaults to slog.Default() if nil.
func NewService(meta MetadataReader, fetch ArticleFetcher, sink Sink, slots SlotAcquirer, log *slog.Logger, opts ...Option) *Service {
	if log == nil {
		log = slog.Default()
	}
	s := &Service{meta: meta, fetch: fetch, sink: sink, slots: slots, log: log.With("component", "par2-repair")}
	for _, o := range opts {
		o(s)
	}
	if s.sem == nil {
		s.sem = make(chan struct{}, DefaultMaxConcurrentRepairs)
	}
	return s
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

	// Refuse oversize files up front: reconstruction holds the whole file in RAM
	// (~2× its size), so above the ceiling we fall back to ARR re-download.
	if s.maxFileBytes > 0 && meta.GetFileSize() > s.maxFileBytes {
		return nil, fmt.Errorf("%w: %d bytes > %d", ErrFileTooLarge, meta.GetFileSize(), s.maxFileBytes)
	}

	// Bound concurrent reconstructions so N simultaneous large-file heals can't
	// OOM the host. This is separate from the connection-admission slot below.
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	case <-ctx.Done():
		return nil, ctx.Err()
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

	// 3. Reconstruct and sink in a single streaming pass. The engine fetches
	// each segment once, scatters present bodies straight into the PAR2 slice
	// grid (peak ~1× the file size, not the ~2× a separate body cache would
	// add), classifies holes (permanently-missing or wrong-sized articles), and
	// verifies every rebuilt slice before sinking. s.fetch satisfies par2.Fetcher
	// directly, so no intermediate cache is materialised.
	res, err := par2.RepairFileSegments(ctx, rs, segs, s.fetch, s.sink)
	if err != nil {
		return nil, err
	}
	if len(res.Recovered) == 0 {
		// Nothing actually missing — the original failure may have been
		// transient. Report a no-op so the caller can simply retry the read.
		return &Outcome{}, nil
	}
	s.log.InfoContext(ctx, "PAR2 self-heal succeeded",
		"file", virtualPath,
		"recovered_segments", len(res.Recovered),
		"slices_fixed", res.SlicesFixed)
	return &Outcome{
		RecoveredSegments: res.Recovered,
		SlicesFixed:       res.SlicesFixed,
		MissingSegments:   len(res.Recovered),
	}, nil
}

// ProbeRepairable cheaply determines, via NNTP STAT, whether virtualPath has
// missing segments and whether its PAR2 recovery data can cover them — without
// downloading any article bodies (the PAR2 files themselves are small and are
// fetched to count recovery slices). It gates proactive self-heal at stream
// start so that healthy files cost only a STAT sweep.
//
// needsRepair is true when at least one data segment is permanently missing.
// repairable is true when a single-file PAR2 set matches the file and carries
// at least as many recovery slices as the missing-slice count. A nil error with
// needsRepair == false means the file is healthy.
func (s *Service) ProbeRepairable(ctx context.Context, virtualPath string) (needsRepair, repairable bool, err error) {
	prober, ok := s.fetch.(ArticleProber)
	if !ok {
		return false, false, ErrProbeUnsupported
	}

	meta, err := s.meta.ReadFileMetadata(virtualPath)
	if err != nil {
		return false, false, fmt.Errorf("repair: read metadata: %w", err)
	}
	if meta == nil {
		return false, false, ErrNoMetadata
	}
	segs := buildSegments(meta.GetSegmentData())
	if len(segs) == 0 {
		return false, false, ErrNoSegments
	}

	// STAT every data segment. A transient probe error aborts the decision
	// (caller skips proactive heal) rather than mislabelling articles missing.
	missing := make(map[string]bool)
	for _, seg := range segs {
		exists, perr := prober.Probe(ctx, seg.MessageID)
		if perr != nil {
			return false, false, fmt.Errorf("repair: probe segment %s: %w", seg.MessageID, perr)
		}
		if !exists {
			missing[seg.MessageID] = true
		}
	}
	if len(missing) == 0 {
		return false, true, nil // healthy: nothing to do
	}

	// Holes exist — can PAR2 cover them? Anything that disqualifies the set
	// (no PAR2, unparseable, multi-file, length mismatch, insufficient recovery)
	// reports needsRepair=true, repairable=false so the caller falls back.
	if len(meta.GetPar2Files()) == 0 {
		return true, false, nil
	}
	rs, err := s.loadRecoverySet(ctx, meta.GetPar2Files())
	if err != nil {
		return true, false, nil
	}
	if len(rs.RecoveryFileIDs) != 1 {
		return true, false, nil
	}
	fd := rs.Files[rs.RecoveryFileIDs[0]]
	if fd == nil || int64(fd.Length) != meta.GetFileSize() {
		return true, false, nil
	}
	missingSlices, _ := par2.MissingSlices(rs, segs, missing)
	repairable = missingSlices > 0 && len(rs.Recovery) >= missingSlices
	return true, repairable, nil
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
