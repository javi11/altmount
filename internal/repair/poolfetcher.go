package repair

import (
	"context"
	"errors"
	"fmt"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v4"
)

// PoolFetcher implements ArticleFetcher against the live NNTP connection pool,
// using the priority lane (the same lane live streaming uses) so a repair pulls
// articles the same way a normal read would. nntppool already fails over across
// configured providers, so a (nil, true, nil) "missing" result means the
// article was absent from every provider, not just the primary.
type PoolFetcher struct {
	mgr pool.Manager
}

// NewPoolFetcher wraps a pool.Manager as an ArticleFetcher.
func NewPoolFetcher(mgr pool.Manager) *PoolFetcher { return &PoolFetcher{mgr: mgr} }

// Fetch returns the decoded body of messageID. A permanently-missing article
// (ErrArticleNotFound, after nntppool has exhausted all providers) is reported
// as (nil, true, nil); transient/operational failures are returned as errors.
func (p *PoolFetcher) Fetch(ctx context.Context, messageID string) ([]byte, bool, error) {
	cp, err := p.mgr.GetPool()
	if err != nil {
		return nil, false, err
	}
	body, err := cp.BodyPriority(ctx, messageID)
	if err != nil {
		if errors.Is(err, nntppool.ErrArticleNotFound) {
			return nil, true, nil
		}
		return nil, false, err
	}
	if body == nil {
		// Defensive: nntppool guards its own (body != nil) accesses, so a nil
		// body with a nil error is not contractually impossible. Treat it as a
		// transient fault rather than panicking on body.Bytes.
		return nil, false, fmt.Errorf("repair: nil article body for %s", messageID)
	}
	return body.Bytes, false, nil
}

// Probe reports whether messageID exists on any provider without downloading
// the body (NNTP STAT). A (false, nil) result means the article is permanently
// missing across all providers; a non-nil error is transient/operational and
// must NOT be interpreted as "missing". This backs the cheap availability sweep
// that gates proactive self-heal at stream start.
func (p *PoolFetcher) Probe(ctx context.Context, messageID string) (bool, error) {
	cp, err := p.mgr.GetPool()
	if err != nil {
		return false, err
	}
	if _, err := cp.Stat(ctx, messageID); err != nil {
		if errors.Is(err, nntppool.ErrArticleNotFound) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
