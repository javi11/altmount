package pool

import (
	"context"
	"io"

	"github.com/javi11/nntppool/v4"
)

// NntpClient is the narrow surface of the underlying nntppool.Client that the
// rest of AltMount calls through Manager.GetPool. Defining it here lets tests
// inject a deterministic fake (see internal/testsupport/fakepool) without
// standing up real NNTP connections, and pins exactly which operations the
// streaming, import, validation, and metrics paths depend on.
//
// Implementations must be safe for concurrent use. The production
// implementation is *nntppool.Client; the contract below intentionally mirrors
// its signatures so the existing client satisfies the interface without an
// adapter.
//
// Keep this interface small. Anything that needs a behavior not listed here
// should add the method explicitly so callers stay observable.
type NntpClient interface {
	// Body fetches an article body via the default (non-priority) lane.
	// Used by the importer to download NZB segments during scanning.
	Body(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)

	// BodyAsync fetches an article body asynchronously, streaming the decoded
	// payload to w. The returned channel yields exactly one BodyResult.
	BodyAsync(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) <-chan nntppool.BodyResult

	// BodyPriority fetches an article body via the priority lane. Streaming
	// reads use this so live playback isn't queued behind a background import.
	BodyPriority(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)

	// BodyBackground fetches an article body via the background lane: served
	// only when nothing priority or normal is queued and capped per provider
	// while foreground traffic is recent. PAR2 repair reads whole releases
	// through it so playback and imports stay ahead of it.
	BodyBackground(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)

	// BodyStreamPriority fetches an article on the priority lane, writing
	// decoded bytes to w as each wire read is decoded so a reader can serve
	// the head of an article before its tail arrives. Bytes on the result is
	// nil. After partial delivery the pool does not fail over to another
	// provider; callers retry with a fresh writer.
	BodyStreamPriority(ctx context.Context, messageID string, w io.Writer, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)

	// Stat checks whether an article exists on at least one provider without
	// downloading the body. Used by health checks and validation.
	Stat(ctx context.Context, messageID string) (*nntppool.StatResult, error)

	// StatMany checks the existence of many articles concurrently, streaming a
	// result per message-id as each completes. Used by health checks and
	// fast-fail import validation to batch existence sweeps instead of
	// issuing one Stat per segment.
	StatMany(ctx context.Context, messageIDs []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult

	// Stats returns a snapshot of pool/provider statistics used by the metrics
	// tracker and the system handlers.
	Stats() nntppool.ClientStats
}

// Compile-time assertion: the real client must satisfy the narrow interface.
// If nntppool changes a signature, this line will fail to build and the
// interface above must be updated to match.
var _ NntpClient = (*nntppool.Client)(nil)
