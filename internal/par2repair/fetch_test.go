package par2repair

import (
	"context"
	"testing"

	"github.com/javi11/nntppool/v4"
)

// fakeStatClient satisfies BodyClient plus the stat surface PoolFetcher
// probes for; liveness is answered from the articles set.
type fakeStatClient struct {
	articles map[string]bool
}

func (c *fakeStatClient) Body(_ context.Context, messageID string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	if !c.articles[messageID] {
		return nil, nntppool.ErrArticleNotFound
	}
	return &nntppool.ArticleBody{}, nil
}

func (c *fakeStatClient) StatMany(_ context.Context, messageIDs []string, _ nntppool.StatManyOptions) <-chan nntppool.StatManyResult {
	out := make(chan nntppool.StatManyResult, len(messageIDs))
	go func() {
		defer close(out)
		for _, id := range messageIDs {
			if c.articles[id] {
				out <- nntppool.StatManyResult{MessageID: id, Result: &nntppool.StatResult{MessageID: id}}
			} else {
				out <- nntppool.StatManyResult{MessageID: id, Err: nntppool.ErrArticleNotFound}
			}
		}
	}()
	return out
}

func TestPoolFetcherStatIDs(t *testing.T) {
	client := &fakeStatClient{articles: map[string]bool{"live@x": true}}
	f := NewPoolFetcher(func() (BodyClient, error) { return client, nil }, nil)

	missing, err := f.StatIDs(context.Background(), []string{"live@x", "gone@x"})
	if err != nil {
		t.Fatal(err)
	}
	if missing["live@x"] || !missing["gone@x"] {
		t.Fatalf("missing = %v", missing)
	}
}

// bodyOnlyClient has no stat surface; StatIDs must degrade to a no-op.
type bodyOnlyClient struct{}

func (bodyOnlyClient) Body(_ context.Context, _ string, _ ...func(nntppool.YEncMeta)) (*nntppool.ArticleBody, error) {
	return &nntppool.ArticleBody{}, nil
}

func TestPoolFetcherStatIDsWithoutStatSupport(t *testing.T) {
	f := NewPoolFetcher(func() (BodyClient, error) { return bodyOnlyClient{}, nil }, nil)

	missing, err := f.StatIDs(context.Background(), []string{"a@x"})
	if err != nil {
		t.Fatal(err)
	}
	if missing != nil {
		t.Fatalf("missing = %v, want nil (capability absent)", missing)
	}
}
