package par2repair

import (
	"context"
	"fmt"

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
// normal request lane, budget-gated like imports.
type PoolFetcher struct {
	client BodyClient
	budget ConnBudget // optional; nil skips budget gating
}

// NewPoolFetcher builds a fetcher over a pool client. budget may be nil.
func NewPoolFetcher(client BodyClient, budget ConnBudget) *PoolFetcher {
	return &PoolFetcher{client: client, budget: budget}
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
	body, err := p.client.Body(ctx, messageID)
	if err != nil {
		return nil, err
	}
	return body.Bytes, nil
}
