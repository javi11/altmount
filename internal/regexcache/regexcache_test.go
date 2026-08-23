package regexcache

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGet_CompilesCaseInsensitiveByDefault(t *testing.T) {
	re, err := Get("cam")
	require.NoError(t, err)
	require.NotNil(t, re)
	assert.True(t, re.MatchString("Movie.2024.CAM.1080p"))

	re2, err := Get("cam")
	require.NoError(t, err)
	assert.Same(t, re, re2, "second call must return the cached instance")
}

func TestGet_RespectsExplicitFlags(t *testing.T) {
	re, err := Get("(?s)a.b")
	require.NoError(t, err)
	require.NotNil(t, re)
	assert.True(t, re.MatchString("a\nb"))
}

func TestGet_EmptyPattern(t *testing.T) {
	re, err := Get("")
	assert.NoError(t, err)
	assert.Nil(t, re)
}

func TestGet_InvalidPatternNotCached(t *testing.T) {
	re, err := Get("[unclosed")
	assert.Error(t, err)
	assert.Nil(t, re)

	mu.Lock()
	_, cached := cache["[unclosed"]
	mu.Unlock()
	assert.False(t, cached, "failed compiles must not poison the cache")

	// A later valid compile of the same text still works after eviction.
	re2, err := Get("(?i)[unclosedx]")
	require.NoError(t, err)
	require.NotNil(t, re2)
}

func TestGet_EvictsWhenFull(t *testing.T) {
	mu.Lock()
	prev := cache
	cache = make(map[string]*regexp.Regexp)
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		cache = prev
		mu.Unlock()
	})

	for i := 0; i < maxEntries+10; i++ {
		pattern := fmt.Sprintf("unique%d", i)
		re, err := Get(pattern)
		require.NoError(t, err)
		require.NotNil(t, re)
	}

	mu.Lock()
	size := len(cache)
	mu.Unlock()
	assert.LessOrEqual(t, size, maxEntries, "cache must never exceed the cap")
}
