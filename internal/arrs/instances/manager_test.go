package instances

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/config"
)

func TestEnsureCategoryExistsInConfig_NewCategorySetsType(t *testing.T) {
	m := &Manager{}
	cfg := &config.Config{}

	m.ensureCategoryExistsInConfig(context.Background(), cfg, "radarr-sqp1", "radarr")

	if len(cfg.SABnzbd.Categories) != 1 {
		t.Fatalf("want 1 category, got %d", len(cfg.SABnzbd.Categories))
	}
	cat := cfg.SABnzbd.Categories[0]
	if cat.Name != "radarr-sqp1" {
		t.Errorf("name = %q, want %q", cat.Name, "radarr-sqp1")
	}
	if cat.Type != "radarr" {
		t.Errorf("type = %q, want %q", cat.Type, "radarr")
	}
}

func TestEnsureCategoryExistsInConfig_BackfillsTypeOnExisting(t *testing.T) {
	m := &Manager{}
	cfg := &config.Config{
		SABnzbd: config.SABnzbdConfig{
			Categories: []config.SABnzbdCategory{
				{Name: "sonarr-4k", Dir: "sonarr-4k"}, // Type missing — pre-fix install
			},
		},
	}

	m.ensureCategoryExistsInConfig(context.Background(), cfg, "sonarr-4k", "sonarr")

	if len(cfg.SABnzbd.Categories) != 1 {
		t.Fatalf("want 1 category, got %d", len(cfg.SABnzbd.Categories))
	}
	if got := cfg.SABnzbd.Categories[0].Type; got != "sonarr" {
		t.Errorf("type = %q, want %q (should have been backfilled)", got, "sonarr")
	}
}

func TestEnsureCategoryExistsInConfig_PreservesExistingType(t *testing.T) {
	m := &Manager{}
	cfg := &config.Config{
		SABnzbd: config.SABnzbdConfig{
			Categories: []config.SABnzbdCategory{
				{Name: "shared", Dir: "shared", Type: "radarr"}, // user already set
			},
		},
	}

	// Caller claims it's sonarr; existing user-set value must win.
	m.ensureCategoryExistsInConfig(context.Background(), cfg, "shared", "sonarr")

	if got := cfg.SABnzbd.Categories[0].Type; got != "radarr" {
		t.Errorf("type = %q, want %q (existing value must be preserved)", got, "radarr")
	}
}

func TestEnsureCategoryExistsInConfig_EmptyCategoryNameDefaultsButNoType(t *testing.T) {
	m := &Manager{}
	cfg := &config.Config{}

	m.ensureCategoryExistsInConfig(context.Background(), cfg, "", "radarr")

	if len(cfg.SABnzbd.Categories) != 1 {
		t.Fatalf("want 1 category, got %d", len(cfg.SABnzbd.Categories))
	}
	cat := cfg.SABnzbd.Categories[0]
	if cat.Name != "default" {
		t.Errorf("name = %q, want %q", cat.Name, "default")
	}
	if cat.Type != "radarr" {
		t.Errorf("type = %q, want %q", cat.Type, "radarr")
	}
}
