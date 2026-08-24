package api

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/prowlarr"
)

// Real release names captured from a live addon query for
// tt5537002 (Killers of the Flower Moon), plus the canonical metadata
// Cinemeta resolves for that IMDb ID.
const kotfmCanonicalTitle = "Killers of the Flower Moon"
const kotfmCanonicalYear = "2023"

func TestReleaseMatchesContent_KotFMLive(t *testing.T) {
	tests := []struct {
		name   string
		title  string
		want   bool
		reason string
	}{
		{
			name:  "library remux (CiNEPHiLES)",
			title: "Killers of the Flower Moon (2023) - [Remux-2160p][TrueHD Atmos 7.1][DV HDR10][HEVC]-CiNEPHiLES",
			want:  true,
		},
		{
			name:  "standard PROPER BluRay encode",
			title: "Killers.of.the.Flower.Moon.2023.PROPER.BluRay.1080p.TrueHD.Atmos.7.1.AVC.REPACK-iRO",
			want:  true,
		},
		{
			name:  "bilingual Turkish release carrying full English title",
			title: "Killers.of.the.Flower.Moon.2023.Dolunay.Katilleri.ATVP.WEB-DL.1080p.H.264.DDP5.1-TURG",
			want:  true,
		},
		{
			name:  "unusual tag-first ordering rescued by token coverage",
			title: "REMUX.2160p.Killers.of.the.Flower.Moon.TrueHD.Atmos.7.1.HEVC.DV-CiNEPHiLES",
			want:  true,
		},
		{
			name:   "wrong movie entirely (althub indexer mislabel)",
			title:  "American.Dilemma.2023.1080p.WEB.h264-BAE",
			want:   false,
			reason: "zero title overlap",
		},
		{
			name:   "typo'd title missing 'Flower'",
			title:  "Killers.Of.The.Moon.2023.1080p.BluRay.REMUX.AVC.TrueHD.7.1-UnKn0wn",
			want:   false,
			reason: "coverage 2/3 below floor and clean-title inequality",
		},
		{
			name:   "extras pack",
			title:  "Killers.Of.The.Flower.Moon.2023.EXTRAS.COMPLETE.BLURAY-BAKED",
			want:   false,
			reason: "junk pattern",
		},
		{
			name:   "full disc package",
			title:  "Killers.of.the.Flower.Moon.2023.COMPLETE.BLURAY-UNTOUCHED",
			want:   false,
			reason: "junk pattern",
		},
		{
			name:   "clearly conflicting year",
			title:  "Killers.of.the.Flower.Moon.2019.1080p.BluRay.x264-FAKE",
			want:   false,
			reason: "year off by more than one",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := releaseMatchesContent("movie", tt.title, kotfmCanonicalTitle, kotfmCanonicalYear, 0, 0)
			if got != tt.want {
				t.Errorf("releaseMatchesContent(%q) = %v, want %v (%s)", tt.title, got, tt.want, tt.reason)
			}
		})
	}
}

func TestIsJunkRelease(t *testing.T) {
	junk := []string{
		"Show.2023.EXTRAS.COMPLETE.BLURAY-BAKED",
		"Movie.2023.COMPLETE.BLURAY-UNTOUCHED",
		"Movie.2023.PROPER.COMPLETE.BLURAY-BAKED",
		"Movie.2023.Sample.a.b.c",
		"EXTRAS-Movie.2023",
	}
	for _, name := range junk {
		if !isJunkRelease(name) {
			t.Errorf("isJunkRelease(%q) = false, want true", name)
		}
	}
	clean := []string{
		"The.Complete.Series.2020.S01.1080p", // "complete" alone without bluray is fine
		"Sampling.The.Past.2021.1080p.WEB-DL",
	}
	for _, name := range clean {
		if isJunkRelease(name) {
			t.Errorf("isJunkRelease(%q) = true, want false", name)
		}
	}
}

func TestReleaseMatchesContent_FailOpenWithoutMetadata(t *testing.T) {
	if !releaseMatchesContent("movie", "Anything.At.All.2023.1080p", "", "", 0, 0) {
		t.Fatal("gate must fail open when canonical metadata is unavailable")
	}
}

func TestTokenCoverage(t *testing.T) {
	expected := titleTokens("Killers of the Flower Moon")
	if len(expected) != 3 {
		t.Fatalf("expected 3 content tokens (stopwords dropped), got %v", expected)
	}
	full := titleTokens("killers flower moon extras")
	if c := tokenCoverage(full, expected); c != 1 {
		t.Errorf("coverage = %v, want 1", c)
	}
	partial := titleTokens("killers moon")
	if c := tokenCoverage(partial, expected); c >= 0.8 {
		t.Errorf("coverage = %v, want < %v", c, minReleaseTokenCoverage)
	}
}

// TestFilterIntegration_OrderingKeepsCachedFirst verifies the wiring shape used
// inside searchStremioReleases: cached releases bypass the gate, uncached ones
// are filtered.
func TestFilterIntegration_OrderingKeepsCachedFirst(t *testing.T) {
	cached := prowlarr.NZBResult{Title: "Some.Cached.Release.Name.2023.1080p-GROUP"}
	fresh := prowlarr.NZBResult{Title: "Killers.of.the.Flower.Moon.2023.PROPER.BluRay.1080p-GROUP"}
	junk := prowlarr.NZBResult{Title: "American.Dilemma.2023.1080p.WEB.h264-BAE"}

	results := []prowlarr.NZBResult{cached, fresh, junk}
	kept := make([]prowlarr.NZBResult, 0, len(results))
	var droppedNames []string
	for _, r := range results {
		if r == cached { // trusted import bypasses the gate
			kept = append(kept, r)
			continue
		}
		if releaseMatchesContent("movie", r.Title, kotfmCanonicalTitle, kotfmCanonicalYear, 0, 0) {
			kept = append(kept, r)
			continue
		}
		droppedNames = append(droppedNames, r.Title)
	}
	if len(kept) != 2 || len(droppedNames) != 1 || droppedNames[0] != junk.Title {
		t.Fatalf("unexpected filtering result: kept=%d dropped=%v", len(kept), droppedNames)
	}
}

func TestReleaseMatchesContentAnimeAliasesAndAbsolute(t *testing.T) {
	romaji := "Meitantei Conan"
	aliases := []string{"Detective Conan", "Case Closed"}
	ctx := releaseMatchContext{aliases: aliases, isAnime: true}
	absCtx := releaseMatchContext{
		aliases:     aliases,
		isAnime:     true,
		episodeMeta: seriesEpisodeMeta{absolute: map[int]map[int]int{1: {1: 1, 670: 670}, 5: {23: 523}, 99: {1210: 1210}}},
	}

	cases := []struct {
		name    string
		release string
		season  int
		episode int
		ctx     releaseMatchContext
		want    bool
	}{
		{"fansub absolute matches catalog absolute", "[SubsPlease] Detective Conan - 1210 (1080p)", 99, 1210, absCtx, true},
		{"erai-raws absolute matches catalog absolute", "[Erai-raws] Detective Conan - 1210 [1080p CR WEB-DL AVC AAC]", 99, 1210, absCtx, true},
		{"toonshub EP prefix matches catalog absolute", "Detective.Conan.EP1210.Episode.1210.1080p.NF.WEB-DL.JPN.AAC2.0.H.264.MSubs-ToonsHub", 99, 1210, absCtx, true},
		{"english alias title match via standard path", "Detective.Conan.S01E01.The.Roller.Coaster.Murder.Case.1080p.CR.WEB-DL.VARYG", 1, 1, ctx, true},
		{"legacy baseline without aliases or anime flag rejects", "[SubsPlease] Detective Conan - 1210 (1080p)", 99, 1210, releaseMatchContext{}, false},
		// Known looseness (pre-existing): once an English alias exists, the
		// token-coverage fallback accepts absolute-numbered releases even
		// without the anime path. Documented, not introduced here.
		{"alias coverage accepts absolute without anime flag", "[SubsPlease] Detective Conan - 1210 (1080p)", 99, 1210, ctx, true},
		{"unrelated series sharing S/E rejected", "One.Piece.S01E01.1080p.WEB-DL.GROUP", 1, 1, absCtx, false},
		{"ARK vs The Ark regression guarded", "ARK.The.Animated.Series.S01E01.1080p.WEB-DL.DDP5.1.H.264-GROUP", 1, 1, releaseMatchContext{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := releaseMatchesContent("series", tc.release, romaji, "", tc.season, tc.episode, tc.ctx); got != tc.want {
				t.Fatalf("releaseMatchesContent(%q) = %v, want %v", tc.release, got, tc.want)
			}
		})
	}
}

func TestResolveSeriesTitleAliasesCacheNilSafe(t *testing.T) {
	if resolveSeriesTitleAliases(context.Background(), "") != nil {
		t.Fatal("empty imdb must yield nil aliases")
	}
}
