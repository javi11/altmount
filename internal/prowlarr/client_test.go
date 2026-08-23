package prowlarr

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMatchKeywordOrPattern_TS_NoFalsePositiveOnDTS(t *testing.T) {
	releaseTitle := "Infidelity.in.Suburbia.2017.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON"

	// "TS" should not match DTS or DTS-HD
	assert.False(t, MatchKeywordOrPattern(releaseTitle, "TS"), "TS keyword should not match DTS-HD")
	assert.False(t, MatchKeywordOrPattern(releaseTitle, "CAM"), "CAM keyword should not match unrelated title")
	assert.False(t, MatchesExcludeKeywords(releaseTitle, []string{"CAM", "TeleSync", "TS", ".sample", "sample", "Korean Dub", "HDCAM", "HDTS"}),
		"DTS-HD REMUX release should not be blacklisted by default exclude keywords")
}

func TestMatchKeywordOrPattern_LegitimateMatches(t *testing.T) {
	tests := []struct {
		title   string
		keyword string
		want    bool
	}{
		// TS / Telesync tests
		{"Movie.2024.TS.1080p.x264", "TS", true},
		{"Movie_2024_TS_1080p", "TS", true},
		{"Movie-2024-TS-1080p", "TS", true},
		{"Movie 2024 [TS] 1080p", "TS", true},
		{"TS.Movie.2024", "TS", true},
		{"Movie.2024.TS", "TS", true},
		{"Movie.2024.TELESYNC.1080p", "TeleSync", true},
		{"Movie.2024.HDTS.1080p", "HDTS", true},

		// Words containing 'ts' substring - must NOT match 'TS' keyword
		{"Infidelity.in.Suburbia.2017.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON", "TS", false},
		{"Streets.Of.Fire.1984.1080p.BluRay.DTS-HD.MA.5.1", "TS", false},
		{"Knights.Of.The.Zodiac.2023.1080p.BluRay.x264", "TS", false},
		{"Cats.2019.1080p.BluRay", "TS", false},
		{"Drafts.2020.1080p.WEB-DL", "TS", false},
		{"The.Ghosts.Of.Monday.2022.1080p", "TS", false},
		{"Two.Tickets.To.Greece.2022.1080p", "TS", false},

		// CAM tests
		{"Movie.2024.CAM.1080p", "CAM", true},
		{"Movie.2024.HDCAM.1080p", "HDCAM", true},
		{"Became.Human.2020.1080p", "CAM", false},
		{"Scam.1992.S01.1080p", "CAM", false},
		{"Camille.2008.1080p", "CAM", false},
		{"Dreamcatcher.2003.1080p", "CAM", false},

		// Multi-word phrase tests
		{"Squid.Game.S01.Korean.Dub.1080p", "Korean Dub", true},
		{"Squid.Game.S01.Korean Dub.1080p", "Korean Dub", true},
		{"Squid.Game.S01.Korean-Dub.1080p", "Korean Dub", true},
		{"Squid.Game.S01.Korean_Dub.1080p", "Korean Dub", true},

		// Sample tests
		{"Movie.2024.1080p.sample.mkv", "sample", true},
		{"Movie.2024.1080p.sample.mkv", ".sample", true},
		{"Movie.2024.1080p-sample.mkv", "sample", true},

		// Explicit regex patterns
		{"Movie.2024.TS.1080p", `\b(cam|ts)\b`, true},
		{"Infidelity.in.Suburbia.2017.1080p.BluRay.REMUX.AVC.DTS-HD.MA.5.1-EPSiLON", `\b(cam|ts)\b`, false},
	}

	for _, tt := range tests {
		t.Run(tt.title+"_"+tt.keyword, func(t *testing.T) {
			got := MatchKeywordOrPattern(tt.title, tt.keyword)
			assert.Equal(t, tt.want, got, "MatchKeywordOrPattern(%q, %q)", tt.title, tt.keyword)
		})
	}
}
