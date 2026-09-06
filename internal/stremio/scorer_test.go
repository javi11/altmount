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

	// 3. DTS-HD Remux release with TS in ExcludeKeywords - MUST NOT BE EXCLUDED
	cfgWithTS := &StreamScoringConfig{
		Preset:          "trash_recommended",
		ExcludeKeywords: []string{"CAM", "TeleSync", "TS", ".sample", "sample"},
		CustomFormats: []TrashCustomFormat{
			{
				ID:       "remux_1080p",
				Name:     "1080p Remux",
				Category: "source",
				Pattern:  `\b1080p\b.*\b(remux|bdremux)\b|\b(remux|bdremux)\b.*\b1080p\b`,
				Score:    350,
				Enabled:  true,
			},
			{
				ID:       "dts_hd_ma",
				Name:     "DTS-HD MA",
				Category: "audio",
				Pattern:  `\b(dts[ ._-]hd([ ._-]ma)?|dts[ ._-]?x)\b`,
				Score:    200,
				Enabled:  true,
			},
			{
				ID:       "tier1_groups",
				Name:     "Tier 1 Groups",
				Category: "release_group",
				Pattern:  `-(FLUX|FraMeSToR|EPSiLON|DON)\b`,
				Score:    150,
				Enabled:  true,
			},
		},
	}
	title3 := "Infidelity.in.Suburbia.2017.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON"
	eval3 := EvaluateRelease(title3, cfgWithTS)
	assert.False(t, eval3.Excluded, "DTS-HD MA release must not be excluded by 'TS' keyword")
	assert.Equal(t, 700, eval3.Score) // 350 + 200 + 150
	assert.Contains(t, eval3.MatchedFormats, "1080p Remux")
	assert.Contains(t, eval3.MatchedFormats, "DTS-HD MA")
	assert.Contains(t, eval3.MatchedFormats, "Tier 1 Groups")

	// 4. Actual TS release with TS in ExcludeKeywords - MUST BE EXCLUDED
	title4 := "Infidelity.in.Suburbia.2017.TS.x264-GROUP"
	eval4 := EvaluateRelease(title4, cfgWithTS)
	assert.True(t, eval4.Excluded)
	assert.Equal(t, "Matched exclude keyword: TS", eval4.ExcludeReason)
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
