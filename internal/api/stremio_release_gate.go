package api

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/javi11/altmount/internal/stremio"
	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"unicode"
)

// minReleaseTokenCoverage is the fraction of the canonical title's significant
// words (stopwords removed) that must appear in a release name for it to be
// considered relevant. The exact-title path via stremio.MatchesMovie handles
// well-formed names; this floor only rescues unusual-but-legitimate orderings.
const minReleaseTokenCoverage = 0.8

// releaseJunkPattern matches release names that are not feature presentations:
// extras packs, samples, and full-disc packages.
var releaseJunkPattern = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])(extras|sample|complete[ ._-]?blu[ ._-]?ray)(?:$|[^a-z0-9])`)

// titleStopwords are common words excluded from release-name token comparison;
// they carry too little meaning to count toward relevance.
var titleStopwords = map[string]bool{
	"of": true, "the": true, "a": true, "an": true, "and": true,
}

// isJunkRelease reports whether a release name describes a non-feature
// package (extras disc, sample clip, or full Blu-ray structure dump).
func isJunkRelease(releaseName string) bool {
	return releaseJunkPattern.MatchString(releaseName)
}

// titleTokens normalizes a title into significant lowercase word tokens,
// mirroring stremio.CleanSeriesTitle's normalization while preserving word
// boundaries so overlap can be measured.
func titleTokens(s string) []string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	normStr, _, _ := transform.String(t, s)
	normStr = strings.ToLower(normStr)
	for _, sep := range []string{".", "_", "-", "+", "/", "\\", ":", "(", ")", "[", "]", ",", "'"} {
		normStr = strings.ReplaceAll(normStr, sep, " ")
	}
	tokens := strings.Fields(normStr)
	out := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if len(tok) < 2 || titleStopwords[tok] {
			continue
		}
		// Keep alphanumeric-only forms so punctuation remnants don't skew counts.
		clean := strings.Map(func(r rune) rune {
			if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				return r
			}
			return -1
		}, tok)
		if len(clean) >= 2 {
			out = append(out, clean)
		}
	}
	return out
}

// tokenCoverage reports the fraction of expected tokens present in the
// release tokens.
func tokenCoverage(release, expected []string) float64 {
	if len(expected) == 0 {
		return 1
	}
	present := make(map[string]bool, len(release))
	for _, tok := range release {
		present[tok] = true
	}
	matched := 0
	for _, tok := range expected {
		if present[tok] {
			matched++
		}
	}
	return float64(matched) / float64(len(expected))
}

// releaseMatchesContent reports whether a release plausibly belongs to the
// requested content. The authoritative check reuses stremio's own movie/series
// matchers (exact cleaned-title equality plus year proximity); the token-
// coverage fallback only rescues legitimate releases whose scene naming
// defeats the strict parser. Off-topic results — wrong films, typo'd titles,
// extras packs — never pass either path.
// releaseMatchContext carries optional per-request metadata that widens the
// relevance gate: alternative series titles (localized names for anime) and
// the franchise-absolute episode number for shows whose releases use
// absolute numbering.
type releaseMatchContext struct {
	aliases     []string
	episodeMeta seriesEpisodeMeta
	isAnime     bool
}

// absoluteEpisode resolves the franchise-absolute number for the requested
// catalog season/episode (0 when unknown).
func (c releaseMatchContext) absoluteEpisode(season, episode int) int {
	return c.episodeMeta.absoluteFor(season, episode)
}

func releaseMatchesContent(streamType, releaseName, expectedTitle, expectedYear string, season, episode int, matchCtx ...releaseMatchContext) bool {
	var ctx releaseMatchContext
	if len(matchCtx) > 0 {
		ctx = matchCtx[0]
	}
	if strings.TrimSpace(expectedTitle) == "" {
		// Canonical metadata unavailable; fail open rather than returning
		// nothing during a metadata-provider outage.
		return true
	}
	if isJunkRelease(releaseName) {
		return false
	}

	expectedYearNum, _ := strconv.Atoi(strings.TrimSpace(expectedYear))

	titles := make([]string, 0, 1+len(ctx.aliases))
	if strings.TrimSpace(expectedTitle) != "" {
		titles = append(titles, expectedTitle)
	}
	for _, alias := range ctx.aliases {
		if strings.TrimSpace(alias) != "" {
			titles = append(titles, alias)
		}
	}

	switch streamType {
	case "series":
		for _, candidate := range titles {
			if stremio.MatchesSeries(releaseName, candidate, season, episode, expectedYearNum) {
				return true
			}
			// Anime absolute-numbering path: fansub packs carry the
			// franchise-wide episode number with no season marker. Retry
			// against the catalog episode's absolute number (season unset so
			// MatchesSeries applies its absolute rule).
			if abs := ctx.absoluteEpisode(season, episode); ctx.isAnime && abs > 0 &&
				stremio.MatchesSeries(releaseName, candidate, 0, abs, expectedYearNum) {
				return true
			}
		}
	default:
		for _, candidate := range titles {
			if stremio.MatchesMovie(releaseName, candidate, expectedYearNum) {
				return true
			}
		}
	}

	parsed := stremio.ParseReleaseTitle(releaseName)
	// Tokenize the full release name rather than parsed.SeriesTitle: scene
	// names sometimes open with quality tags, which truncates the parser's
	// series-title extraction to a single word. Coverage is directional —
	// extra release-side tokens never inflate the score. Every candidate
	// title gets its own coverage check.
	for _, candidate := range titles {
		coverage := tokenCoverage(titleTokens(releaseName), titleTokens(candidate))
		if coverage < minReleaseTokenCoverage {
			continue
		}
		// Coverage passed: only a clearly conflicting year still disqualifies.
		if parsed.Year > 0 && expectedYearNum > 0 {
			diff := parsed.Year - expectedYearNum
			if diff < 0 {
				diff = -diff
			}
			if diff > 1 {
				continue
			}
		}
		return true
	}
	return false
}
