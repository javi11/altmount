# Implementation Plan: Paginated History Search for Reliable Blocklisting

## Objective
Implement a paginated search mechanism in `blocklistRadarrMovieFile` and `blocklistSonarrEpisodeFile` so the scanner can search through multiple history pages (e.g., up to 5,000 records) to find grab events for legacy files, ensuring the blocklist trigger works even for older releases.

## Scope & Impact
- **Manager:** Modify `internal/arrs/scanner/manager.go` to add a pagination loop for history fetching.
- **Reliability:** This will make the blocklisting mechanism significantly more reliable for existing media files that don't yet have an associated `DownloadID` in the `file_health` table.

## Implementation Steps
### 1. Paginated History Fetcher
- Create a helper function or logic block within `blocklistRadarrMovieFile` and `blocklistSonarrEpisodeFile` to iterate through pages.
- Use `starr.PageReq` with `PageSize=1000`.
- Continue fetching pages until the `DownloadID` (or `grabbed` event) is found or the total history limit (e.g., 5,000 records) is reached.

### 2. Update logic
- Integrate the loop into both ARR scanners.
- Ensure the loop handles termination correctly when history pages are exhausted.

## Verification
- Confirm that AltMount can now find the `09bcb934-b84e-4a1c-b6d0-e49045c98e47` grab event for the Abbott Elementary FLUX release when triggering a manual repair.
- Verify through logs that the scanner is now checking multiple pages.
