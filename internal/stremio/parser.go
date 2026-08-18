package stremio

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

var (
	// Matches season/episode patterns like S01E02, S1E2, 1x02, Season 1 Episode 2, S01E01-E02, S01E01E02
	seasonEpRegex = regexp.MustCompile(`(?i)[\. _\-\(\[]+(?:s(\d{1,2})[\. _\-]?e(\d{1,3})(?:[\. _\-]*(?:e|[-~])(\d{1,3}))?|(\d{1,2})x(\d{1,3})|season[\. _\-]?(\d{1,2})[\. _\-]?episode[\. _\-]?(\d{1,3}))`)

	// Matches season packs like S01, Season 1 (without trailing episode)
	seasonPackRegex = regexp.MustCompile(`(?i)[\. _\-\(\[]+(?:s(\d{1,2})|season[\. _\-]?(\d{1,2}))(?:[\. _\-\)\]]+|$)`)

	// Matches 4-digit release year: (1990)-(2099)
	yearRegex = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)

	// Matches common quality/source/codec tags to clean title
	qualityTagsRegex = regexp.MustCompile(`(?i)[\. _\-\(\[]+(?:2160p|1080p|1080i|720p|480p|576p|uhd|4k|bluray|blu-ray|remux|web-dl|webrip|web|hdtv|dvd|dvdr|x264|x265|h264|h265|hevc|avc|av1|hdr|hdr10|hdr10\+|dv|dolby[\. _\-]?vision|atmos|ddp5\.1|dd5\.1|aac|dts|dts-hd|truehd)`)
)

// ParsedRelease contains parsed metadata from a scene/P2P release title.
type ParsedRelease struct {
	SeriesTitle  string
	CleanTitle   string
	Season       int
	EpisodeStart int
	EpisodeEnd   int
	Year         int
	IsSeasonPack bool
}

// ParseReleaseTitle parses a release title into its structured components.
func ParseReleaseTitle(title string) ParsedRelease {
	res := ParsedRelease{}
	clean := strings.TrimSpace(title)
	if clean == "" {
		return res
	}

	// 1. Try matching Season & Episode
	if loc := seasonEpRegex.FindStringSubmatchIndex(clean); loc != nil {
		rawSeries := clean[:loc[0]]
		res.SeriesTitle = strings.TrimSpace(strings.Trim(rawSeries, ".-_ ()[]"))

		matches := seasonEpRegex.FindStringSubmatch(clean)
		if len(matches) > 3 && matches[1] != "" && matches[2] != "" {
			res.Season, _ = strconv.Atoi(matches[1])
			res.EpisodeStart, _ = strconv.Atoi(matches[2])
			if matches[3] != "" {
				res.EpisodeEnd, _ = strconv.Atoi(matches[3])
			} else {
				res.EpisodeEnd = res.EpisodeStart
			}
		} else if len(matches) > 5 && matches[4] != "" && matches[5] != "" {
			res.Season, _ = strconv.Atoi(matches[4])
			res.EpisodeStart, _ = strconv.Atoi(matches[5])
			res.EpisodeEnd = res.EpisodeStart
		} else if len(matches) > 7 && matches[6] != "" && matches[7] != "" {
			res.Season, _ = strconv.Atoi(matches[6])
			res.EpisodeStart, _ = strconv.Atoi(matches[7])
			res.EpisodeEnd = res.EpisodeStart
		}
	} else if loc := seasonPackRegex.FindStringSubmatchIndex(clean); loc != nil {
		// Season Pack
		rawSeries := clean[:loc[0]]
		res.SeriesTitle = strings.TrimSpace(strings.Trim(rawSeries, ".-_ ()[]"))
		res.IsSeasonPack = true

		matches := seasonPackRegex.FindStringSubmatch(clean)
		if len(matches) > 1 && matches[1] != "" {
			res.Season, _ = strconv.Atoi(matches[1])
		} else if len(matches) > 2 && matches[2] != "" {
			res.Season, _ = strconv.Atoi(matches[2])
		}
	} else if loc := yearRegex.FindStringSubmatchIndex(clean); loc != nil {
		// Movie: title is before year
		rawSeries := clean[:loc[0]]
		res.SeriesTitle = strings.TrimSpace(strings.Trim(rawSeries, ".-_ ()[]"))
	} else if loc := qualityTagsRegex.FindStringSubmatchIndex(clean); loc != nil {
		// Unstructured: title is before quality tags
		res.SeriesTitle = strings.TrimSpace(strings.Trim(clean[:loc[0]], ".-_ ()[]"))
	} else {
		res.SeriesTitle = clean
	}

	// 2. Extract Year if present
	if yMatches := yearRegex.FindStringSubmatch(clean); len(yMatches) > 1 {
		res.Year, _ = strconv.Atoi(yMatches[1])
	}

	// Strip trailing year from series title if present (e.g., "The Ark (2023)" -> "The Ark")
	if res.SeriesTitle != "" {
		res.SeriesTitle = yearRegex.ReplaceAllString(res.SeriesTitle, "")
		res.SeriesTitle = strings.TrimSpace(strings.Trim(res.SeriesTitle, ".-_ ()[]"))
	}

	res.CleanTitle = CleanSeriesTitle(res.SeriesTitle)
	return res
}

// CleanSeriesTitle normalizes a title matching Sonarr/Prowlarr's CleanSeriesTitle:
// 1. Removes diacritics/accents
// 2. Converts to lowercase
// 3. Strips leading articles ("the", "a", "an")
// 4. Removes year if present
// 5. Removes all non-alphanumeric characters
func CleanSeriesTitle(title string) string {
	s := strings.TrimSpace(title)
	if s == "" {
		return ""
	}

	// Strip diacritics
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normStr, _, _ := transform.String(t, s)

	normStr = strings.ToLower(normStr)

	// Replace common separators with spaces
	for _, sep := range []string{".", "_", "-", "+", "/", "\\", ":", "(", ")", "[", "]"} {
		normStr = strings.ReplaceAll(normStr, sep, " ")
	}

	// Strip year
	normStr = yearRegex.ReplaceAllString(normStr, " ")

	// Strip leading articles
	trimmed := strings.TrimSpace(normStr)
	if strings.HasPrefix(trimmed, "the ") {
		trimmed = strings.TrimPrefix(trimmed, "the ")
	} else if strings.HasPrefix(trimmed, "a ") {
		trimmed = strings.TrimPrefix(trimmed, "a ")
	} else if strings.HasPrefix(trimmed, "an ") {
		trimmed = strings.TrimPrefix(trimmed, "an ")
	}

	// Keep only alphanumeric characters [a-z0-9]
	var b strings.Builder
	for _, r := range trimmed {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchesSeries verifies if a release matches the requested TV series title and episode.
func MatchesSeries(releaseTitle, expectedTitle string, expectedSeason, expectedEpisode, expectedYear int) bool {
	if expectedTitle == "" {
		return true
	}

	parsed := ParseReleaseTitle(releaseTitle)

	// 1. Title verification (Prowlarr/Sonarr 1:1 clean title comparison)
	cleanExpected := CleanSeriesTitle(expectedTitle)
	if cleanExpected != "" && parsed.CleanTitle != "" {
		if parsed.CleanTitle != cleanExpected {
			// Title mismatch (e.g. "arktheanimatedseries" vs "ark")
			return false
		}
	}

	// 2. Episode verification
	if expectedSeason > 0 && expectedEpisode > 0 {
		if parsed.Season > 0 {
			if parsed.Season != expectedSeason {
				return false
			}
			if !parsed.IsSeasonPack {
				if parsed.EpisodeStart > 0 {
					if expectedEpisode < parsed.EpisodeStart || expectedEpisode > parsed.EpisodeEnd {
						return false
					}
				}
			}
		}
	}

	// 3. Disambiguation Year verification (if both release and query specify year)
	if expectedYear > 0 && parsed.Year > 0 {
		// Allow +/- 1 year tolerance for international/theatrical broadcast dates
		if parsed.Year < expectedYear-1 || parsed.Year > expectedYear+1 {
			return false
		}
	}

	return true
}

// MatchesMovie verifies if a release matches the requested movie title and year.
func MatchesMovie(releaseTitle, expectedTitle string, expectedYear int) bool {
	if expectedTitle == "" {
		return true
	}

	parsed := ParseReleaseTitle(releaseTitle)
	cleanExpected := CleanSeriesTitle(expectedTitle)

	if cleanExpected != "" && parsed.CleanTitle != "" {
		if parsed.CleanTitle != cleanExpected {
			return false
		}
	}

	if expectedYear > 0 && parsed.Year > 0 {
		if parsed.Year < expectedYear-1 || parsed.Year > expectedYear+1 {
			return false
		}
	}

	return true
}
