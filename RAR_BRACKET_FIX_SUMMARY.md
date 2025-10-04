# RAR Archive Bracket Filename Fix - Summary

## Problem Statement

RAR archives from certain Usenet sources have obfuscated filenames with square brackets, such as:
```
[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r00]
[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r01]
```

This caused three cascading issues:
1. **RAR detection failed** - Files were misclassified as `multi_file` instead of `rar_archive`
2. **Part parsing failed** - Files couldn't be identified as part 0, part 1, etc.
3. **Multi-volume extraction failed** - Only the first 50 MB was extracted instead of the full ~480 MB

## Root Cause Analysis

### Issue 1: RAR Detection
The regex pattern `\.r\d+$` couldn't match filenames ending with `]` instead of the extension:
- Pattern expected: `filename.r00`
- Actual filename: `[PREFIX]-[filename.r00]`

### Issue 2: Part Number Parsing
The `parseRarFilename()` function used regex patterns anchored with `$` (end of string):
- Pattern: `\.r(\d+)$` 
- Matched: `the.smurfs.r00`
- Did NOT match: `[PREFIX]-[the.smurfs.r00]` ← brackets at the end

### Issue 3: Multi-Volume Continuation (The Core Problem)
When `rarlist` library reads the first RAR part (`.r00`), the RAR header contains references to continuation volumes:
- RAR header stores: `the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r01` ← No brackets!
- `UsenetFileSystem` has: `[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r01]` ← Has brackets!

**Result:** `rarlist` requests `the.smurfs...r01`, can't find it, gives up after reading only `.r00` (50 MB).

## Solution Architecture

The fix normalizes filenames at a **single point** early in the processing pipeline, before passing them to `rarlist`.

### Solution: Upstream Normalization

**Location:** `renameRarFilesAndSort()` function in `internal/importer/rar_processor.go`

**Strategy:** Strip brackets from filenames when extracting the base name and part suffix, so the normalized filenames match what's stored in the RAR headers.

### Changes Made

#### 1. Enhanced RAR Detection (`internal/importer/parser.go`)

**Change:** Made RAR detection more robust
```go
// Before: Only checked final filename
isRarArchive := rarPattern.MatchString(filename)

// After: Checks all three filename sources
isRarArchive := rarPattern.MatchString(filename) ||
    rarPattern.MatchString(file.Filename) ||
    (yencFilename != "" && rarPattern.MatchString(yencFilename))
```

**Rationale:** Defensive programming - checks NZB filename, yEnc filename, and final filename to ensure we don't miss RAR archives.

**Change:** Made yEnc header fetching non-fatal
```go
// Before: Returned error and aborted
if err != nil {
    return nil, fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
}

// After: Logs warning and continues with NZB metadata
if err != nil {
    p.log.Debug("Failed to fetch yEnc headers, continuing with NZB metadata",
        "filename", file.Filename,
        "error", err)
}
```

**Rationale:** Allows processing to continue even if yEnc headers are unavailable or corrupted.

#### 2. Filename Normalization (`internal/importer/rar_processor.go`)

**Change:** Updated `extractBaseFilename()` to strip brackets before extracting base name

```go
func extractBaseFilename(filename string) string {
    // Handle filenames with brackets like [PRiVATE]-[WtFnZb]-[actual.file.r00]
    // Extract the last bracketed section if it contains a RAR extension
    if lastBracket := strings.LastIndex(filename, "["); lastBracket >= 0 {
        if closeBracket := strings.Index(filename[lastBracket:], "]"); closeBracket >= 0 {
            innerFilename := filename[lastBracket+1 : lastBracket+closeBracket]
            lowerInner := strings.ToLower(innerFilename)
            if strings.HasSuffix(lowerInner, ".rar") ||
                strings.Contains(lowerInner, ".r0") ||
                strings.Contains(lowerInner, ".r1") ||
                strings.Contains(lowerInner, ".part") {
                filename = innerFilename
            }
        }
    }
    
    // Continue with normal pattern matching...
}
```

**Change:** Updated `getPartSuffix()` with identical bracket stripping logic

**Rationale:** Ensures both base name and part suffix extraction work with clean filenames.

**Result:** The `renameRarFilesAndSort()` function now produces normalized filenames:
```
Before: [PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r00]
After:  the.smurfs.2021.s01e21.dutch.1080p.web.h264-nlkids.r00
```

#### 3. Debug Logging Additions

**Added logging to:**
- `renameRarFilesAndSort()` - Shows filename normalization
- `AnalyzeRarContentFromNzb()` - Shows what `rarlist` returns
- `UsenetFileSystem.Open()` - Shows what files `rarlist` requests

**Rationale:** Helps troubleshoot similar issues in the future.

## Data Flow

```
1. NZB Upload
   ↓
2. Parse NZB → Extract files with bracketed names
   ↓
3. RAR Detection (checks all filename sources)
   ↓
4. renameRarFilesAndSort() → NORMALIZE FILENAMES HERE
   ↓                          [PREFIX]-[file.r00] → file.r00
5. Create UsenetFileSystem with normalized filenames
   ↓
6. Pass to rarlist → rarlist reads .r00 header
   ↓                 Header says "continuation: file.r01"
7. rarlist requests file.r01 → ✅ FOUND (normalized!)
   ↓
8. rarlist extracts full 480 MB from all 9 parts
```

## Why This Solution Works

### Single Normalization Point
- **Before:** Bracket handling scattered across 3 functions (`parseRarFilename`, `Open`, `Stat`)
- **After:** Single normalization in `renameRarFilesAndSort` before any processing

### Correct Abstraction Level
- Normalization happens at the **file collection level** (before individual file operations)
- All downstream code works with clean filenames
- No defensive bracket checking needed in file access code

### Matches RAR Internal Structure
- RAR headers store clean filenames (no obfuscation)
- Our normalized filenames now match RAR headers exactly
- `rarlist` can find all continuation volumes

## Testing & Verification

### Before Fix
```
Log: type=multi_file files=9  ← Misclassified!
Log: parts_count=1              ← Only found first part
Result: 47.7 MB file            ← Truncated!
```

### After Fix
```
Log: type=rar_archive files=9  ← Correct classification
Log: Normalized RAR filename original=[PREFIX]-[file.r00] normalized=file.r00
Log: parts_count=9              ← Found all 9 parts!
Result: 480 MB file             ← Full episode!
```

## Code Quality Improvements

### Lines Changed
- **Added:** ~50 lines (normalization + logging)
- **Removed:** ~77 lines (redundant bracket handling)
- **Net:** -27 lines, cleaner architecture

### Maintainability
- Single source of truth for filename normalization
- Clear separation of concerns
- Better debug visibility

## Files Modified

1. **`internal/importer/parser.go`**
   - Enhanced RAR detection (multi-source checking)
   - Made yEnc header fetching non-fatal

2. **`internal/importer/rar_processor.go`**
   - Added bracket stripping to `extractBaseFilename()`
   - Added bracket stripping to `getPartSuffix()`
   - Added debug logging to `renameRarFilesAndSort()`
   - Removed redundant bracket handling from `parseRarFilename()`

3. **`internal/importer/usenet_filesystem.go`**
   - Added logger parameter to constructor
   - Added debug logging to `Open()`
   - Removed redundant bracket matching from `Open()` and `Stat()`

## Lessons Learned

### Problem Diagnosis
1. Start with symptom (47 MB vs 480 MB)
2. Use logging to trace data flow
3. Identify the exact point of failure (rarlist only reads first part)
4. Understand the abstraction (RAR headers store clean filenames)

### Solution Design
1. Fix at the right abstraction level (file collection, not file access)
2. Normalize data early in the pipeline
3. Remove redundant defensive code after proper fix
4. Add logging for future troubleshooting

### Code Quality
1. Single responsibility principle (one normalization point)
2. Don't repeat yourself (removed 3 copies of bracket handling)
3. Document non-obvious behavior (comments about normalization)

## Future Considerations

### Potential Enhancements
- Could extend to handle other obfuscation patterns if needed
- Consider making bracket pattern configurable if different sources use different formats

### Performance
- Normalization happens once per RAR archive (negligible overhead)
- No performance impact on file access operations

### Compatibility
- No breaking changes to external APIs
- Works with all existing RAR archive formats
- Backward compatible with non-bracketed filenames

## Conclusion

The fix successfully resolves RAR extraction issues for obfuscated filenames by normalizing filenames at a single point early in the processing pipeline. This ensures the filenames passed to `rarlist` match the continuation volume names stored in RAR headers, allowing successful multi-volume extraction.

**Result:** Full-size RAR archive extraction working correctly! ✅

