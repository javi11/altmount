package stremio

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestEvaluateRelease_MatchingAndExclusions(t *testing.T) {
	cfg := &StreamScoringConfig{
		Preset: "trash_recommended",
		ExcludeKeywords: []string{"CAM", "TeleSync", "sample"},
		CustomFormats: []TrashCustomFormat{
			{
				ID:       "remux_4k",
				Name:     "4K UHD Remux",
				Category: "source",
				Pattern:  `\b(2160p|4k)\b.*\b(remux|bdremux)\b|\b(remux|bdremux)\b.*\b(2160p|4k)\b`,
				Score:    500,
				Enabled:  true,
			},
			{
				ID:       "dv",
				Name:     "Dolby Vision",
				Category: "hdr",
				Pattern:  `\b(dv|dovi|dolby[ ._-]?vision)\b`,
				Score:    350,
				Enabled:  true,
			},
			{
				ID:       "atmos",
				Name:     "TrueHD Atmos",
				Category: "audio",
				Pattern:  `\b(truehd[ ._-]?atmos|truehd|atmos)\b`,
				Score:    250,
				Enabled:  true,
			},
		},
	}

	// 1. Healthy High-Score Release
	title1 := "Dune.Part.Two.2024.2160p.UHD.Remux.DV.HDR10+.TrueHD.Atmos.7.1-FraMeSToR"
	eval1 := EvaluateRelease(title1, cfg)
	assert.False(t, eval1.Excluded)
	assert.Equal(t, 1100, eval1.Score)
	assert.Contains(t, eval1.MatchedFormats, "4K UHD Remux")
	assert.Contains(t, eval1.MatchedFormats, "Dolby Vision")
	assert.Contains(t, eval1.MatchedFormats, "TrueHD Atmos")

	// 2. Blacklisted Release
	title2 := "Dune.Part.Two.2024.CAM.1080p.x264-WAR"
	eval2 := EvaluateRelease(title2, cfg)
	assert.True(t, eval2.Excluded)
	assert.Contains(t, eval2.ExcludeReason, "CAM")
}

func TestRankAndFilterReleases(t *testing.T) {
	cfg := &StreamScoringConfig{
		ExcludeKeywords: []string{"CAM"},
		CustomFormats: []TrashCustomFormat{
			{
				ID:       "remux",
				Name:     "Remux",
				Pattern:  `\bremux\b`,
				Score:    500,
				Enabled:  true,
			},
			{
				ID:       "webdl",
				Name:     "WEB-DL",
				Pattern:  `\bweb[ ._-]?dl\b`,
				Score:    200,
				Enabled:  true,
			},
		},
	}

	now := time.Now()
	releases := []SearchResult{
		{Title: "Movie.2024.1080p.WEB-DL-GROUP", Indexer: "NZBGeek", PublishDate: now.Add(-1 * time.Hour)},
		{Title: "Movie.2024.1080p.CAM-GROUP", Indexer: "NZBGeek", PublishDate: now},
		{Title: "Movie.2024.2160p.Remux-GROUP", Indexer: "Ninja", PublishDate: now.Add(-2 * time.Hour)},
	}

	ranked := RankAndFilterReleases(releases, cfg, map[string]int{"Ninja": 50})
	assert.Len(t, ranked, 2)
	assert.Equal(t, "Movie.2024.2160p.Remux-GROUP", ranked[0].Title)
	assert.Equal(t, 550, ranked[0].Score) // 500 + 50 bonus
	assert.Equal(t, "Movie.2024.1080p.WEB-DL-GROUP", ranked[1].Title)
	assert.Equal(t, 200, ranked[1].Score)
}
