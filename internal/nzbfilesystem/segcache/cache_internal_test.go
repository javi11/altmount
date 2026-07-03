package segcache

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPutIgnoredWhileLoading guards the loading gate that replaced the
// merge-under-lock path: while the catalog hydrates, Put must be a silent no-op
// so a segment downloaded during cold load neither contends with the load nor
// gets clobbered by the wholesale map assignment in LoadCatalog. Once the gate
// clears, Put works normally again.
func TestPutIgnoredWhileLoading(t *testing.T) {
	cfg := Config{CachePath: t.TempDir(), MaxSizeBytes: 10 * 1024 * 1024}
	c, err := NewSegmentCache(cfg, slog.Default())
	require.NoError(t, err)

	c.loading.Store(true)

	require.NoError(t, c.Put("busy@msg", []byte("data")))
	assert.False(t, c.Has("busy@msg"), "Put must be ignored while catalog is loading")
	assert.EqualValues(t, 0, c.ItemCount())
	assert.EqualValues(t, 0, c.TotalSize())

	c.loading.Store(false)

	require.NoError(t, c.Put("busy@msg", []byte("data")))
	assert.True(t, c.Has("busy@msg"))
}
