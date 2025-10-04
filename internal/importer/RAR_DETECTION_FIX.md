# RAR Detection Fix for Small RAR Archives

## Problem Summary

The Smurfs NZB (12 RAR files: `.rar`, `.r00`, `.r01`, etc.) was being misclassified as `multi_file` instead of `rar_archive`, preventing the RAR processor from extracting the internal MKV file. Meanwhile, Mr Nobody NZB (215 files) was correctly classified as `rar_archive`.

## Root Cause

Two issues in `internal/importer/parser.go`:

1. **Fatal yEnc Header Fetching (Line 195)**: When yEnc headers couldn't be fetched, the function returned an error and failed to process the file entirely, despite a comment saying "log and continue with original sizes".

2. **Single-Source RAR Detection (Line 312)**: The RAR pattern check only examined the "final chosen" filename, not all available filename sources. If yEnc headers provided a filename without RAR extensions while the NZB filename had them, RAR detection would fail.

## Changes Made

### 1. Made yEnc Header Fetching Non-Fatal

**Before:**
```go
firstPartHeaders, err := p.fetchYencHeaders(file.Segments[0], nil)
if err != nil {
    // If we can't fetch yEnc headers, log and continue with original sizes
    return nil, fmt.Errorf("failed to fetch first segment yEnc part size: %w", err)
}
yencFilename = firstPartHeaders.FileName
yencFileSize = int64(firstPartHeaders.FileSize)
```

**After:**
```go
firstPartHeaders, err := p.fetchYencHeaders(file.Segments[0], nil)
if err != nil {
    // If we can't fetch yEnc headers, log and continue with original sizes
    p.log.Debug("Failed to fetch yEnc headers, continuing with NZB metadata",
        "filename", file.Filename,
        "error", err)
} else {
    yencFilename = firstPartHeaders.FileName
    yencFileSize = int64(firstPartHeaders.FileSize)
}
```

**Impact:** Files can now be processed even when yEnc headers are unavailable due to network issues, missing articles, or provider problems.

### 2. Enhanced RAR Detection to Check All Filename Sources

**Before:**
```go
// Check if this is a RAR file
isRarArchive := rarPattern.MatchString(filename)
```

**After:**
```go
// Check if this is a RAR file
// Check multiple filename sources to ensure we catch RAR archives
// even when yEnc headers provide different names
isRarArchive := rarPattern.MatchString(filename) ||
    rarPattern.MatchString(file.Filename) ||
    (yencFilename != "" && rarPattern.MatchString(yencFilename))

p.log.Debug("RAR detection",
    "final_filename", filename,
    "nzb_filename", file.Filename,
    "yenc_filename", yencFilename,
    "is_rar_archive", isRarArchive)
```

**Impact:** RAR archives are now detected regardless of which filename source (yEnc, NZB, or metadata) contains the RAR extensions.

## RAR Pattern Verification

The existing regex pattern correctly matches all RAR file variations:

```regex
(?i)\.r(ar|\d+)$|\.part\d+\.rar$
```

**Matches:**
- `.rar` - Main RAR file
- `.r00`, `.r01`, `.r02`, ... `.r99` - Standard multi-volume RAR parts
- `.r0`, `.r1`, `.r2` - Old-style single-digit RAR parts
- `.part01.rar`, `.part02.rar` - Alternative naming scheme

## Testing Recommendations

To verify the fix works with the Smurfs NZB:

1. **Test with original Smurfs NZB**: Re-import and verify it's now detected as `type=rar_archive`
2. **Check logs**: Look for the new debug message "RAR detection" showing all three filename sources
3. **Verify extraction**: Confirm the internal MKV file is exposed as a virtual file
4. **Test edge cases**:
   - NZBs where yEnc headers fail to fetch
   - Mixed naming patterns (`.rar`/`.r00` vs `.part01.rar`)
   - Obfuscated filenames with RAR extensions in NZB but not yEnc

## Expected Behavior After Fix

**Smurfs NZB (12 files):**
- ✅ Detected as `type=rar_archive` (not `multi_file`)
- ✅ Triggers "Processing RAR archive with content analysis"
- ✅ Exposes internal MKV file as virtual file
- ✅ RAR parts remain accessible for repair/verification

**Mr Nobody NZB (215 files):**
- ✅ Continues to work as before
- ✅ No regression in functionality

## Files Modified

- `internal/importer/parser.go`:
  - Lines 193-202: Made yEnc header fetching non-fatal
  - Lines 313-324: Enhanced RAR detection with multi-source checking and debug logging

