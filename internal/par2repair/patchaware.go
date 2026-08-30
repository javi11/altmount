package par2repair

import "context"

// PatchChecker is implemented by fetchers that can report whether a locally
// repaired copy of an article exists. Planning uses it to keep already-fixed
// articles out of the dead set, so a completed repair is never re-planned and
// re-downloaded on the next cycle.
type PatchChecker interface {
	HasPatch(messageID string) bool
}

// patchAwareFetcher overlays the patch store on an ArticleFetcher: a patched
// article is alive — Fetch serves the repaired payload and StatIDs never
// reports it missing. Repaired bytes exist only locally, so without this a
// release repaired once would plan (and download) the same repair forever.
type patchAwareFetcher struct {
	inner ArticleFetcher
	store *PatchStore
}

func newPatchAwareFetcher(inner ArticleFetcher, store *PatchStore) *patchAwareFetcher {
	return &patchAwareFetcher{inner: inner, store: store}
}

func (p *patchAwareFetcher) Fetch(ctx context.Context, messageID string) ([]byte, error) {
	if data, ok := p.store.Get(messageID); ok {
		return data, nil
	}
	return p.inner.Fetch(ctx, messageID)
}

// StatIDs implements ArticleStater: patched ids are alive without a wire
// call; the rest delegate to the inner stater when it supports STAT.
func (p *patchAwareFetcher) StatIDs(ctx context.Context, ids []string, onResult func(done int)) (map[string]bool, error) {
	unknown := make([]string, 0, len(ids))
	patched := 0
	for _, id := range ids {
		if p.store.Has(id) {
			patched++
			continue
		}
		unknown = append(unknown, id)
	}
	stater, ok := p.inner.(ArticleStater)
	if !ok {
		return nil, nil
	}
	missing, err := stater.StatIDs(ctx, unknown, func(done int) {
		if onResult != nil {
			onResult(patched + done)
		}
	})
	if err != nil {
		return nil, err
	}
	return missing, nil
}

func (p *patchAwareFetcher) HasPatch(messageID string) bool {
	return p.store.Has(messageID)
}
