package database

import (
	"context"
	"database/sql"
	"fmt"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupStoreRefTestDB(t *testing.T) *StoreRefRepository {
	t.Helper()
	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name()))
	require.NoError(t, err)
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS nzb_store_refs (
			store_path TEXT NOT NULL PRIMARY KEY,
			ref_count  INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL DEFAULT (datetime('now'))
		)
	`)
	require.NoError(t, err)

	return NewStoreRefRepository(db, DialectSQLite)
}

func TestStoreRefRepository_IncStoreRef(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	storePath := "/store/abc.nzbz"

	// First increment: row should be inserted with ref_count = 1.
	require.NoError(t, repo.IncStoreRef(ctx, storePath))
	count, err := repo.GetStoreRefCount(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(1), count)

	// Second increment: ref_count should become 2.
	require.NoError(t, repo.IncStoreRef(ctx, storePath))
	count, err = repo.GetStoreRefCount(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Third increment: ref_count should become 3.
	require.NoError(t, repo.IncStoreRef(ctx, storePath))
	count, err = repo.GetStoreRefCount(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

func TestStoreRefRepository_GetStoreRefCount_NotFound(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	count, err := repo.GetStoreRefCount(ctx, "/store/nonexistent.nzbz")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestStoreRefRepository_DecStoreRef_Decrement(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	storePath := "/store/dec.nzbz"

	// Set up: increment 3 times.
	require.NoError(t, repo.IncStoreRef(ctx, storePath))
	require.NoError(t, repo.IncStoreRef(ctx, storePath))
	require.NoError(t, repo.IncStoreRef(ctx, storePath))

	// Decrement: should return 2.
	count, err := repo.DecStoreRef(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(2), count)

	// Verify via Get.
	got, err := repo.GetStoreRefCount(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(2), got)
}

func TestStoreRefRepository_DecStoreRef_DeletesRowAtZero(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	storePath := "/store/zero.nzbz"

	// Set up: increment once, then decrement back to zero.
	require.NoError(t, repo.IncStoreRef(ctx, storePath))

	count, err := repo.DecStoreRef(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count, "decrementing to zero should return 0")

	// Row must be gone.
	got, err := repo.GetStoreRefCount(ctx, storePath)
	require.NoError(t, err)
	assert.Equal(t, int64(0), got, "row should have been deleted")
}

func TestStoreRefRepository_DecStoreRef_NoRow(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	// Decrementing a non-existent row should not error and should return 0.
	count, err := repo.DecStoreRef(ctx, "/store/ghost.nzbz")
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestStoreRefRepository_MultipleStores(t *testing.T) {
	repo := setupStoreRefTestDB(t)
	ctx := context.Background()

	pathA := "/store/a.nzbz"
	pathB := "/store/b.nzbz"

	require.NoError(t, repo.IncStoreRef(ctx, pathA))
	require.NoError(t, repo.IncStoreRef(ctx, pathA))
	require.NoError(t, repo.IncStoreRef(ctx, pathB))

	countA, err := repo.GetStoreRefCount(ctx, pathA)
	require.NoError(t, err)
	assert.Equal(t, int64(2), countA)

	countB, err := repo.GetStoreRefCount(ctx, pathB)
	require.NoError(t, err)
	assert.Equal(t, int64(1), countB)

	// Decrement A once; B should be unaffected.
	newA, err := repo.DecStoreRef(ctx, pathA)
	require.NoError(t, err)
	assert.Equal(t, int64(1), newA)

	countB, err = repo.GetStoreRefCount(ctx, pathB)
	require.NoError(t, err)
	assert.Equal(t, int64(1), countB, "store B must be unaffected")
}
