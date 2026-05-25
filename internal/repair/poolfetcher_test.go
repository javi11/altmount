package repair

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nntppool/v4"
)

// nilBodyClient returns a (nil, nil) body from BodyPriority — the contract gap
// the Fetch nil-guard defends against. Embedding the interface satisfies the
// rest of NntpClient without implementing every method.
type nilBodyClient struct{ pool.NntpClient }

func (nilBodyClient) BodyPriority(context.Context, string, ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	return nil, nil
}

type nilBodyManager struct{ pool.Manager }

func (nilBodyManager) GetPool() (pool.NntpClient, error) { return nilBodyClient{}, nil }

// TestPoolFetcherNilBody ensures a nil body with a nil error is surfaced as a
// (transient) error instead of panicking on body.Bytes, and is not mistaken for
// a permanently-missing article.
func TestPoolFetcherNilBody(t *testing.T) {
	pf := NewPoolFetcher(nilBodyManager{})
	data, missing, err := pf.Fetch(context.Background(), "<x@altmount>")
	if err == nil {
		t.Fatal("expected an error for a nil article body, got nil")
	}
	if missing {
		t.Fatal("a nil body must not be reported as a missing article")
	}
	if data != nil {
		t.Fatalf("expected nil data, got %d bytes", len(data))
	}
}
