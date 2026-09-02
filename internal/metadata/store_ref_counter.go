package metadata

import "context"

// StoreRefCounter tracks reference counts for shared NzbStore files.
// Implementations are provided by the database layer; the nil value is always safe to use.
type StoreRefCounter interface {
	IncStoreRef(ctx context.Context, storePath string) error
	// DecStoreRef decrements the count and reports whether the store was tracked
	// at all. A count of 0 is only meaningful as "no references remain" when the
	// second value is true; an untracked store has an unknown, not zero, ref count.
	DecStoreRef(ctx context.Context, storePath string) (int64, bool, error)
}
