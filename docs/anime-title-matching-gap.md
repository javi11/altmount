# Anime title-matching gap in Stremio stream gate

## Symptom
Stremio requests for classic anime return "No streams available" although
indexers carry releases. Example (2026-08-24):

    tt0131179 (Detective Conan) S01E01
    althub result: Detective.Conan.S01E01.720p.HDTV.x264.The.Roller.Coaster.Murder.Case-TEST
    -> Dropped irrelevant Stremio releases dropped_count=1 kept=-1

## Root cause
resolveSeriesMetadataFromIMDb uses TVMaze's primary name, which for anime is
frequently romaji ("Meitantei Conan"). Release names use another localization
("Detective Conan"). MatchesSeries step 1 compares CleanSeriesTitle 1:1
(mismatch -> reject), and the releaseMatchesContent token-coverage fallback
requires >=0.8 coverage of expected tokens — the release contains only
"conan" (0.5) -> dropped.

## Correct fix (design)
Alias-aware matching:
1. resolveSeriesMetadataFromIMDb additionally fetches
   https://api.tvmaze.com/shows/{id}/akas once per IMDb id (process-cached);
   collect names where country=="en" (fallback: any).
2. Thread []string titles through the Stremio search/scoring path.
3. releaseMatchesContent accepts when MatchesSeries passes for ANY alias;
   strict 1:1 comparison stays per-alias so "ARK: The Animated Series" vs
   "The Ark" remains correctly rejected (verified failing case today when a
   token-sharing relaxation was attempted and reverted).
4. Cache aliases keyed by imdbID; tolerate akas failures (aliases=nil).

## Rejected approach
Token-sharing relaxation inside MatchesSeries (accept on any shared word):
breaks the deliberate ARK/The-Ark disambiguation tests.

## Effort
~150 LoC across tvdb_lookup.go, stremio_addon_handlers.go,
stremio_release_gate.go + tests.
