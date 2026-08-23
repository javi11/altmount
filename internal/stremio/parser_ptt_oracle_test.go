package stremio

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file is a test oracle ported from dreulavelle/PTT ("Parsett",
// https://github.com/dreulavelle/PTT, MIT license), specifically its
// test_episodes.py, test_season.py and test_title.py fixture suites.
//
// PTT parses far more fields than ParsedRelease models, so only the shared
// surface is asserted: extracted title (compared via CleanSeriesTitle on both
// sides, making it separator/article insensitive), season, episode range,
// season-pack detection and year.
//
// Two tiers:
//
//   - TestPTTOracleParity pins behavior where AltMount must agree with PTT.
//   - TestPTTOracleKnownDivergences pins current behavior where AltMount
//     disagrees with PTT. Every case states PTT's expectation and the practical
//     impact on release matching, so future parser work knows exactly which
//     pins to revisit (and which divergences are harmless by design).

type oracleCase struct {
	name        string
	release     string
	wantTitle   string // compared as CleanSeriesTitle(wantTitle) == got.CleanTitle
	wantSeason  int
	wantEpStart int
	wantEpEnd   int // ignored when both episode fields are zero
	wantPack    bool
	wantYear    int // asserted only when non-zero
}

func assertOracle(t *testing.T, tc oracleCase) {
	t.Helper()
	got := ParseReleaseTitle(tc.release)

	if tc.wantTitle != "" {
		assert.Equal(t, CleanSeriesTitle(tc.wantTitle), got.CleanTitle,
			"title mismatch for %q (got SeriesTitle=%q)", tc.release, got.SeriesTitle)
	}
	assert.Equal(t, tc.wantSeason, got.Season, "season mismatch for %q", tc.release)
	assert.Equal(t, tc.wantEpStart, got.EpisodeStart, "episode start mismatch for %q", tc.release)
	if tc.wantEpEnd != 0 || tc.wantEpStart != 0 {
		assert.Equal(t, tc.wantEpEnd, got.EpisodeEnd, "episode end mismatch for %q", tc.release)
	}
	assert.Equal(t, tc.wantPack, got.IsSeasonPack, "pack flag mismatch for %q", tc.release)
	if tc.wantYear != 0 {
		assert.Equal(t, tc.wantYear, got.Year, "year mismatch for %q", tc.release)
	}
}

func TestPTTOracleParity(t *testing.T) {
	par := func(name, release, title string, season, epStart, epEnd int) oracleCase {
		return oracleCase{name: name, release: release, wantTitle: title,
			wantSeason: season, wantEpStart: epStart, wantEpEnd: epEnd}
	}
	pack := func(name, release, title string, season int) oracleCase {
		return oracleCase{name: name, release: release, wantTitle: title,
			wantSeason: season, wantPack: true}
	}

	tests := []oracleCase{
		// --- Standard SxxEyy scene naming ---
		par("standard scene", "24.Legacy.S01E05.720p.HEVC.x265-MeGusta", "24 Legacy", 1, 5, 5),
		par("lowercase", "breaking.bad.s01e01.720p.bluray.x264-reward", "Breaking Bad", 1, 1, 1),
		par("dots inside title", "black-ish.S05E02.1080p..x265.10bit.EAC3.6.0-Qman[UTR].mkv", "black-ish", 5, 2, 2),
		par("apostrophe title", "Friends.S07E20.The.One.With.Rachel's.Big.Kiss.720p.BluRay.2CH.x265.HEVC-PSA.mkv", "Friends", 7, 20, 20),
		par("two digit season", "The Simpsons S28E21 720p HDTV x264-AVS", "The Simpsons", 28, 21, 21),
		par("NxNN form", "Doctor.Who.2005.8x11.Dark.Water.720p.HDTV.x264-FoV", "Doctor Who", 8, 11, 11),
		par("bracketed NxNN", "Lost.[Perdidos].6x05.HDTV.XviD.[www.DivxTotaL.com]", "Lost Perdidos", 6, 5, 5),
		par("parenthesized NxNN", "Smallville (1x02 Metamorphosis).avi", "Smallville", 1, 2, 2),
		par("double dash separators", "Pawn Stars -- 4x13 -- Broadsiding Lincoln.mkv", "Pawn Stars", 4, 13, 13),
		par("acronym title", "S.W.A.T.2017.S08E01.720p.HDTV.x264-SYNCOPY[TGx]", "S W A T", 8, 1, 1),
		par("lowercase snake form", "doctor_who_2005.8x12.death_in_heaven.720p_hdtv_x264-fov", "doctor who", 8, 12, 12),

		// --- Multi-episode ranges (SxxEyy form) ---
		par("dot separated e range", "Game.of.Thrones.S01.e01-02.2160p.UHD.BluRay.x265-Morpheus", "Game of Thrones", 1, 1, 2),
		par("hyphen range", "Marvel's.Agents.of.S.H.I.E.L.D.S02E01-03.Shadows.1080p.WEB-DL.DD5.1", "Marvel's Agents of S.H.I.E.L.D.", 2, 1, 3),
		par("e dash e range", "The Simpsons S01E01-E02 1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole", "The Simpsons", 1, 1, 2),
		par("chained ee", "The Simpsons S01E01E02 1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole", "The Simpsons", 1, 1, 2),
		par("pokemon e range", "Pokémon.S01E01-E04.SWEDISH.VHSRip.XviD-aka", "Pokémon", 1, 1, 4),

		// --- Season packs (including trailing quality tags) ---
		pack("pack with quality", "Archer.S02.1080p.BluRay.DTSMA.AVC.Remux", "Archer", 2),
		pack("bare s pack", "Bron - S4 - 720P - SweSub.mp4", "Bron", 4),
		pack("redundant pack naming", "Seinfeld Season 2 S02 720p AMZN WEBRip x265 HEVC Complete", "Seinfeld", 2),
		pack("mad men style", "Mad Men S02 Season 2 720p 5.1Ch BluRay ReEnc-DeeJayAhmed", "Mad Men", 2),
		pack("season before quality tags", "CSI Crime Scene Investigation S01 720p WEB DL DD5 1 H 264 LiebeIst[rartv]", "CSI Crime Scene Investigation", 1),

		// --- Movies must never yield episodes/seasons ---
		{
			name: "movie no false episode", release: "Mad.Max.Fury.Road.2015.1080p.BluRay.DDP5.1.x265.10bit-GalaxyRG265[TGx]",
			wantTitle: "Mad Max Fury Road", wantSeason: 0, wantYear: 2015,
		},
		{
			name: "joker proper", release: "Joker.2019.PROPER.mHD.10Bits.1080p.BluRay.DD5.1.x265-TMd.mkv",
			wantTitle: "Joker", wantSeason: 0, wantYear: 2019,
		},
		{
			name: "despicable telesync", release: "Despicable.Me.4.2024.D.TELESYNC_14OOMB.avi",
			wantTitle: "Despicable Me 4", wantSeason: 0, wantYear: 2024,
		},
		{
			name: "accented french title", release: "La.famille.bélier",
			wantTitle: "La famille bélier", wantSeason: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) { assertOracle(t, tc) })
	}
}

// TestPTTOracleYearExtraction pins the year cases where AltMount agrees with
// PTT. Year divergences live in TestPTTOracleKnownDivergences.
func TestPTTOracleYearExtraction(t *testing.T) {
	tests := []struct {
		release  string
		wantYear int
	}{
		{"Mad.Max.Fury.Road.2015.1080p.BluRay.DDP5.1.x265.10bit-GalaxyRG265[TGx]", 2015},
		{"Joker.2019.PROPER.mHD.10Bits.1080p.BluRay.DD5.1.x265-TMd.mkv", 2019},
		{"BoJack Horseman [06x01-08 of 16] (2019-2020) WEB-DLRip 720p", 2019},
	}
	for _, tt := range tests {
		t.Run(tt.release, func(t *testing.T) {
			got := ParseReleaseTitle(tt.release)
			require.Equal(t, tt.wantYear, got.Year)
		})
	}
}

// Gap taxonomy observed against the PTT fixtures, ordered by matching impact:
//
//	HIGH  anime absolute numbering ("Naruto Shippuden - 107") never parsed;
//	      padded three-digit seasons (S011E16, One.Piece.S004E111) unrecognized
//	MED   NxNN ranges don't extend ([7x23-24] -> E23 only); episode chains cap
//	      at two (E01-E02-E03 -> E1-2); '+' chaining lost; multi-season spans
//	      (S01-S10) collapse to the first season; in-title-year movies pick the
//	      name year (Wonder Woman 1984 -> year 1984, not 2020)
//	LOW   regional keywords (Cap./Temporada concat, sezon/seriya, "ao" ranges,
//	      plural "Episodes" ranges) fall back to pack or nothing; edition words
//	      (CUSTOM/EXTENDED/INTEGRAL/PPV) stay in extracted titles; snake_case
//	      years yield 0 due to \b word-boundary semantics
//
// Pack fallback softens several of these: "Season 5 Episodes 1-10" and
// "S3 Eps.05-08" still detect season+pack, which the episode selector accepts
// for any episode in that season.
func TestPTTOracleKnownDivergences(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		pttNote  string // what PTT reports
		assertFn func(t *testing.T, got ParsedRelease)
	}{
		// --- HIGH impact ---
		{
			name:    "anime absolute numbering",
			release: "Naruto Shippuden - 107 - Strange Bedfellows.mkv",
			pttNote: "PTT: episode 107 (absolute)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.EpisodeStart)
				assert.Contains(t, got.SeriesTitle, "107")
			},
		},
		{
			name:    "fansub bracket absolute numbering",
			release: "[SubsPlease] Digimon Adventure (2020) - 35 (720p) [4E7BA28A].mkv",
			pttNote: "PTT: episode 35",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "three digit season S011E16",
			release: "S011E16.mkv",
			pttNote: "PTT: S11 E16",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "three digit season One Piece S004E111",
			release: "One.Piece.S004E111.Dash.For.a.Miracle!.Alabasta.Animal.Land!.1080p.NF.WEB-DL.DDP2.0.x264-KQRM.mkv",
			pttNote: "PTT: S4 E111",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},

		// --- MED impact ---
		{
			name:    "NxNN hyphen range not extended",
			release: "Friends - [7x23-24] - The One with Monica and Chandler's Wedding",
			pttNote: "PTT: E23-E24",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 7, got.Season)
				assert.Equal(t, 23, got.EpisodeStart)
				assert.Equal(t, 23, got.EpisodeEnd, "range suffix only supported on SxxEyy form")
			},
		},
		{
			name:    "bracketed NxNN range not extended",
			release: "BoJack Horseman [06x01-08 of 16] (2019-2020) WEB-DLRip 720p",
			pttNote: "PTT: E1-E8",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 6, got.Season)
				assert.Equal(t, 1, got.EpisodeStart)
				assert.Equal(t, 1, got.EpisodeEnd)
			},
		},
		{
			name:    "triple chained E01-E02-E03 stops at second",
			release: "Stargate Universe S01E01-E02-E03.mp4",
			pttNote: "PTT: E1-E3",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 1, got.EpisodeStart)
				assert.Equal(t, 2, got.EpisodeEnd)
			},
		},
		{
			name:    "quintuple chained range also caps at two",
			release: "The Simpsons S01E01-E02-E03-E04-E05 1080p BluRay x265 HEVC 10bit AAC 5.1 Tigole",
			pttNote: "PTT: E1-E5",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 1, got.EpisodeStart)
				assert.Equal(t, 2, got.EpisodeEnd)
			},
		},
		{
			name:    "plus chained S07E25+E26 loses second episode",
			release: "The Office S07E25+E26 Search Committee.mp4",
			pttNote: "PTT: E25+E26",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 25, got.EpisodeStart)
				assert.Equal(t, 25, got.EpisodeEnd)
			},
		},
		{
			name:    "multi season range S01-S10 collapses to first",
			release: "Friends.Complete.Series.S01-S10.720p.BluRay.2CH.x265.HEVC-PSA",
			pttNote: "PTT: seasons 1-10",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.True(t, got.IsSeasonPack)
				assert.Equal(t, 1, got.Season)
			},
		},
		{
			name:    "in-title-year movie picks name year",
			release: "Wonder Woman 1984 (2020) [UHDRemux 2160p DoVi P8 Es-DTSHD AC3 En-AC3].mkv",
			pttNote: "PTT: year 2020 (release year)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 1984, got.Year)
				assert.Equal(t, "wonderwoman", got.CleanTitle)
			},
		},

		// --- LOW impact (pack fallback or cosmetic) ---
		{
			name:    "plural Episodes keyword yields pack only",
			release: "Orange Is The New Black Season 5 Episodes 1-10 INCOMPLETE (LEAKED)",
			pttNote: "PTT: S5, E1-E10",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.True(t, got.IsSeasonPack)
				assert.Equal(t, 5, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "Eps.range form yields pack only",
			release: "MARATHON EPISODES/Orphan Black S3 Eps.05-08.mp4",
			pttNote: "PTT: S3, E5-E8",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.True(t, got.IsSeasonPack)
				assert.Equal(t, 3, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "Ep(range) treated as pack only",
			release: "Vikings.Season.05.Ep(01-10).720p.WebRip.2Ch.x265.PSA",
			pttNote: "PTT: S5 plus E1-E10",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.True(t, got.IsSeasonPack)
				assert.Equal(t, 5, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "all seasons numeric range (1-8)",
			release: "House MD All Seasons (1-8) 720p Ultra-Compressed",
			pttNote: "PTT: seasons 1-8",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.False(t, got.IsSeasonPack)
				assert.Zero(t, got.Season)
			},
		},
		{
			name:    "glued NxNN without separator",
			release: "The.Man.In.The.High.Castle1x01.HDTV.XviD[www.DivxTotaL.com].avi",
			pttNote: "PTT: S1 E1, title 'The Man In The High Castle'",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
				assert.Contains(t, got.SeriesTitle, "1x01")
			},
		},
		{
			name:    "spanish cap concatenation",
			release: "Anatomia De Grey - Temporada 19 [HDTV][Cap.1905][Castellano][www.AtomoHD.nu].avi",
			pttNote: "PTT: S19 (Cap.1905 => season+episode concat)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "cyrillic sezon seriya",
			release: "Zvezdnie.Voiny.Voina.Klonov.3.sezon.22.seria.iz.22.XviD.HDRip.avi",
			pttNote: "PTT: S3 E22",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "portuguese range ao",
			release: "Bleach 10º Temporada - 215 ao 220 - [DB-BR]",
			pttNote: "PTT: S10, E215-E220",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Season)
				assert.Zero(t, got.EpisodeStart)
			},
		},
		{
			name:    "edition words retained before year in movie title",
			release: "Jurassic.World.Dominion.CUSTOM.EXTENDED.2022.2160p.MULTi.VF2.UHD.Blu-ray.REMUX.HDR.DoVi.HEVC.DTS-X.DTS-HDHRA.7.1-MOONLY.mkv",
			pttNote: "PTT: title 'Jurassic World Dominion' (strips CUSTOM/EXTENDED)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, 2022, got.Year)
				assert.Equal(t, "jurassicworlddominioncustomextended", got.CleanTitle)
			},
		},
		{
			name:    "quality-only title overcaptures prefix words",
			release: "Grimm.INTEGRAL.MULTI.COMPLETE.BLURAY-BMTH",
			pttNote: "PTT: title 'Grimm' (strips INTEGRAL/MULTI/COMPLETE/BLURAY)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Equal(t, "grimmintegralmulticomplete", got.CleanTitle)
			},
		},
		{
			name:    "ppv token retained in title",
			release: "UFC.247.PPV.Jones.vs.Reyes.HDTV.x264-PUNCH[TGx]",
			pttNote: "PTT: title 'UFC 247 Jones vs Reyes' (drops PPV)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Contains(t, got.CleanTitle, "ppv")
			},
		},
		{
			name:    "snake_case year not detected",
			release: "doctor_who_2005.8x12.death_in_heaven.720p_hdtv_x264-fov",
			pttNote: "PTT: year 2005 (\\b boundary fails next to underscore)",
			assertFn: func(t *testing.T, got ParsedRelease) {
				assert.Zero(t, got.Year)
			},
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := ParseReleaseTitle(tc.release)
			tc.assertFn(t, got)
		})
	}
}
