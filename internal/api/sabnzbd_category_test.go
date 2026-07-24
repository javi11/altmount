package api

import (
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
)

// buildCategoryPath and validateSABnzbdCategory both build a real directory
// path from category, which is client-reachable via the SABnzbd-emulation
// API with no other validation upstream. Every "return category" fallback
// used to hand it back raw. See the sibling copies of this same bug already
// fixed and tested in internal/importer (resolveCategoryPath) and
// internal/importer/utils (SanitizePathSegment).

func TestBuildCategoryPath_RejectsTraversal(t *testing.T) {
	t.Run("no config manager, traversal category", func(t *testing.T) {
		server := &Server{}
		got := server.buildCategoryPath("../../../etc")
		assert.NotContains(t, got, "..")
		assert.Equal(t, "", got)
	})

	t.Run("no categories configured, traversal category", func(t *testing.T) {
		cfg := &config.Config{}
		server := &Server{configManager: &mockConfigManager{cfg: cfg}}
		got := server.buildCategoryPath("../../../etc")
		assert.NotContains(t, got, "..")
		assert.Equal(t, "", got)
	})

	t.Run("categories configured, category not found, traversal", func(t *testing.T) {
		cfg := &config.Config{
			SABnzbd: config.SABnzbdConfig{
				Categories: []config.SABnzbdCategory{{Name: "movies", Dir: "movies"}},
			},
		}
		server := &Server{configManager: &mockConfigManager{cfg: cfg}}
		got := server.buildCategoryPath("../../../etc")
		assert.NotContains(t, got, "..")
		assert.Equal(t, "", got)
	})

	t.Run("legitimate category still resolves normally", func(t *testing.T) {
		cfg := &config.Config{
			SABnzbd: config.SABnzbdConfig{
				Categories: []config.SABnzbdCategory{{Name: "movies", Dir: "movies"}},
			},
		}
		server := &Server{configManager: &mockConfigManager{cfg: cfg}}
		assert.Equal(t, "movies", server.buildCategoryPath("movies"))
	})
}

func TestValidateSABnzbdCategory_RejectsTraversalWhenUnconfigured(t *testing.T) {
	cfg := &config.Config{} // SABnzbd.Categories empty
	server := &Server{configManager: &mockConfigManager{cfg: cfg}}

	got, err := server.validateSABnzbdCategory("../../../etc")
	assert.Error(t, err, "a traversal category must be rejected at validation, not silently accepted")
	assert.Empty(t, got)
}

func TestValidateSABnzbdCategory_AllowsLegitimateCategoryWhenUnconfigured(t *testing.T) {
	cfg := &config.Config{} // SABnzbd.Categories empty
	server := &Server{configManager: &mockConfigManager{cfg: cfg}}

	got, err := server.validateSABnzbdCategory("movies")
	assert.NoError(t, err)
	assert.Equal(t, "movies", got)
}
