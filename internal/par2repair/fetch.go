package par2repair

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/javi11/nntppool/v4"
)

// BodyClient is the one pool method the fetcher needs: a normal-lane (import)
// article body fetch. Satisfied by pool.NntpClient.
type BodyClient interface {
	Body(ctx context.Context, messageID string, onMeta ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error)
}

// ConnBudget gates fetches on the global import connection budget so repair
// sweeps never starve streaming reads. Satisfied by pool.Manager.
type ConnBudget interface {
	AcquireImportConnection(ctx context.Context) (release func(), err error)
}

// PoolFetcher fetches decoded article payloads through the NNTP pool's
// normal request lane, budget-gated like imports. The client is resolved per
// fetch so the fetcher survives pool reconfiguration and boot ordering.
type PoolFetcher struct {
	getClient func() (BodyClient, error)
	budget    ConnBudget // optional; nil skips budget gating
}

// NewPoolFetcher builds a fetcher over a lazily-resolved pool client
// (typically wrapping pool.Manager.GetPool). budget may be nil.
func NewPoolFetcher(getClient func() (BodyClient, error), budget ConnBudget) *PoolFetcher {
	return &PoolFetcher{getClient: getClient, budget: budget}
}

// ArticleStater is an optional ArticleFetcher capability: bulk article
// existence checks without downloading bodies. Resolvers use it to learn the
// release's full damage before planning, so unrepairable verdicts land in
// seconds instead of after downloading the recovery set.
type ArticleStater interface {
	// StatIDs reports which of ids are confirmed missing on every provider.
	// Transient failures must NOT be reported missing. A nil map with a nil
	// error means the capability is unavailable and the caller should proceed
	// without liveness data.
	StatIDs(ctx context.Context, ids []string) (missing map[string]bool, err error)
}

// statManyClient is the stat surface PoolFetcher probes its pool client for
// (satisfied by *nntppool.Client and pool.NntpClient implementations).
type statManyClient interface {
	StatMany(ctx context.Context, messageIDs []string, opts nntppool.StatManyOptions) <-chan nntppool.StatManyResult
}

// statPerItemBudget bounds a liveness sweep: STATs are single-line round
// trips dispatched dozens-at-a-time, so even a generous per-item share keeps
// the overall deadline in seconds-to-minutes for a full release.
const statPerItemBudget = 250 * time.Millisecond

// StatIDs implements ArticleStater over the pool's StatMany when the client
// supports it; otherwise it reports the capability unavailable (nil, nil).
func (p *PoolFetcher) StatIDs(ctx context.Context, ids []string) (map[string]bool, error) {
	client, err := p.getClient()
	if err != nil {
		return nil, fmt.Errorf("par2repair: nntp pool unavailable: %w", err)
	}
	sc, ok := client.(statManyClient)
	if !ok {
		return nil, nil
	}
	timeout := max(time.Duration(len(ids))*statPerItemBudget, time.Minute)
	statCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	missing := make(map[string]bool)
	for r := range sc.StatMany(statCtx, ids, nntppool.StatManyOptions{}) {
		if errors.Is(r.Err, nntppool.ErrArticleNotFound) {
			missing[r.MessageID] = true
		}
	}
	// A cancelled sweep left ids unchecked; report it rather than a partial
	// view that would understate the damage.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return missing, nil
}

// Fetch implements ArticleFetcher.
func (p *PoolFetcher) Fetch(ctx context.Context, messageID string) ([]byte, error) {
	if p.budget != nil {
		release, err := p.budget.AcquireImportConnection(ctx)
		if err != nil {
			return nil, fmt.Errorf("par2repair: acquire import connection: %w", err)
		}
		defer release()
	}
	client, err := p.getClient()
	if err != nil {
		return nil, fmt.Errorf("par2repair: nntp pool unavailable: %w", err)
	}
	// nntppool adds the angle brackets itself; message IDs here are bare.
	body, err := client.Body(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return body.Bytes, nil
}
