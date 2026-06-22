package metadata

import "context"

// StoreRefCounter tracks reference counts for shared NzbStore files.
// Implementations are provided by the database layer; the nil value is always safe to use.
type StoreRefCounter interface {
	IncStoreRef(ctx context.Context, storePath string) error
	DecStoreRef(ctx context.Context, storePath string) (int64, error)
}
