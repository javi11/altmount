# RAR Multi-Episode Archives - Detection and Limitations

## Overview

AltMount includes intelligent detection for multi-episode RAR archives (season packs) where multiple independent RAR archives are combined into a single NZB with sequential part numbering. This document explains how this detection works, its current limitations, and recommendations for handling these archives.

## Background

### Standard RAR Archive Structure

A typical single-episode RAR archive:
```
Episode1.rar          (part 0)
Episode1.r00          (part 1)
Episode1.r01          (part 2)
...
Episode1.r19          (part 20)
```

A typical multi-episode season pack with separate archives:
```
Episode1.rar, Episode1.r00, ..., Episode1.r19
Episode2.rar, Episode2.r00, ..., Episode2.r19
Episode3.rar, Episode3.r00, ..., Episode3.r19
Episode4.rar, Episode4.r00, ..., Episode4.r19
```

### Problematic Structure: Sequential Multi-Episode Archives

Some NZB uploaders create season packs with **sequential numbering across all episodes**:
```
archive.rar           (part 0)  ← Episode 1 header
archive.r00           (part 1)  ← Episode 1 data
...
archive.r19           (part 20) ← Episode 1 data
archive.r20           (part 21) ← Episode 2 header (EMBEDDED!)
archive.r21           (part 22) ← Episode 2 data
...
archive.r82           (part 83) ← Episode 4 data
```

**The Problem:** Each episode has its own RAR header **embedded in mid-volume files** (`.r20`, `.r42`, `.r62`), but the `rarlist` library used by AltMount requires starting from part 0 (`.rar` or `.r00`).

## Current Implementation

### RAR Header Detection

AltMount now includes intelligent RAR header detection that activates when:
1. First archive extracts successfully (e.g., Episode 1)
2. Remaining parts (63 files) have no part 0
3. System scans first 10 remaining parts for RAR file signatures

**RAR Signature Detection:**
- RAR 4.x: `52 61 72 21 1A 07 00` (`Rar!\x1A\x07\x00`)
- RAR 5.x: `52 61 72 21 1A 07 01 00` (`Rar!\x1A\x07\x01\x00`)
- Scans first 512 bytes of each file

### Log Messages

**Multi-Episode Archive Detected (Partial Extraction Fails):**
```
INFO  Archive extraction complete: archive_number=1, files_extracted=1, parts_used=21
WARN  Archive extraction failed, checking for RAR headers in remaining parts
DEBUG Scanning remaining parts for RAR headers: total_parts=63
INFO  Found RAR header in remaining part: file=archive.r42, offset=0
ERROR Found RAR headers in remaining parts - indicating multi-episode archive with embedded headers
ERROR This archive structure is not fully supported - only first episode can be extracted
ERROR Recommendation: Use SABnzbd to download and extract the complete archive
ERROR Item failed permanently: multi-episode archive with embedded RAR headers
```

**Broken Archive (No Headers Found):**
```
INFO  Archive extraction complete: archive_number=1, files_extracted=1, parts_used=21
WARN  Archive extraction failed, checking for RAR headers in remaining parts
DEBUG Scanning remaining parts for RAR headers: total_parts=63
DEBUG No RAR headers found in remaining parts: parts_checked=10
ERROR multi-volume RAR archive missing part 0 (.rar or .r00) - cannot process
ERROR Item failed permanently after max retries
```

## Limitations

### Current Behavior

For sequential multi-episode archives like Bellicher:
- ✅ **Episode 1 extracts successfully** (uses `.rar`, `.r00` - `.r19`)
- ✅ **Episodes 2-4 are detected** (RAR headers found in remaining parts)
- ❌ **Episodes 2-4 cannot be extracted** (no part 0 available)
- ❌ **Import fails with clear error message** (prevents false-positive success)

**Why Fail Instead of Partial Success?**

Reporting success for partial extractions would cause serious issues:
- **Sonarr/Radarr** would mark the entire season as "downloaded"
- **Plex/Jellyfin** would show all episodes as available (but only E01 exists)
- **Users** would experience playback errors for E02-E04
- **Arr apps** wouldn't retry or use alternative sources

By failing with a clear error message, the Arr applications know to:
- Try alternative NZB sources
- Fall back to SABnzbd for extraction
- Not mark incomplete downloads as successful

### Technical Constraints

1. **`rarlist` Library Requirement:**
   - Requires starting from part 0 (`.rar` or `.r00`)
   - Cannot start analysis from mid-volume files (`.r20`, `.r42`, etc.)
   - This is a fundamental limitation of the library's design

2. **Part Number Mapping:**
   - `.r20` is physically "part 21" but would need to be treated as "part 0" for Episode 2
   - Would require complex virtual remapping of part numbers
   - Segments reference actual part numbers, making remapping non-trivial

3. **Header Location:**
   - RAR headers for episodes 2-4 are embedded at offsets within the sequential volumes
   - `rarlist` expects headers at the start of volume 0

## Workarounds

### For Users

**Option A: Use SABnzbd for Complete Extraction (Recommended)**
- Let SABnzbd download and extract the full NZB
- SABnzbd uses native `unrar` which handles this structure
- Import extracted MKV files to your media server directly
- Benefit: All episodes extracted to disk

**Option B: Re-encode Archive (If You Control the Source)**
- Create separate RAR archives per episode:
  ```
  Episode1.rar, Episode1.r00, ..., Episode1.r19
  Episode2.rar, Episode2.r00, ..., Episode2.r19
  ```
- This allows AltMount to extract all episodes

### For Developers

**Diagnostic Value:**
The current implementation provides valuable diagnostic information:
- Confirms archive structure is valid
- Identifies exactly where embedded headers are located
- Helps users understand why full extraction isn't possible

**Future Enhancement Ideas:**
1. **Virtual Part Remapping:**
   - Detect episode boundaries
   - Create virtual file mappings (`.r20` → `.rar` for Episode 2)
   - Modify segment offsets accordingly
   - Feed remapped parts to `rarlist`

2. **Direct RAR Header Parsing:**
   - Bypass `rarlist` for multi-episode archives
   - Implement custom RAR header parsing
   - Extract file information directly from embedded headers
   - Build segment maps manually

3. **Hybrid Approach:**
   - Extract Episode 1 with current logic
   - For remaining episodes, create temporary "virtual NZB" per episode
   - Process each as if it were a standalone archive

## Example: Bellicher Case Study

### Archive Structure
```
Total NZB Parts: 84 (plus PAR2 files)
Total Size: ~10 GB (compressed)

Episode 1: Parts 0-20 (2.2 GB)
  - archive.rar (RAR header at offset 0) ✅ EXTRACTED
  - archive.r00 through archive.r19

Episode 2: Parts 21-41 (2.2 GB estimated)
  - archive.r20 (no header visible)
  - archive.r21 through archive.r41
  - RAR header likely embedded in one of these parts ❌ NOT EXTRACTED

Episode 3: Parts 42-62 (2.2 GB estimated)
  - archive.r42 (RAR header found at offset 0!) ✅ DETECTED
  - archive.r43 through archive.r61 ❌ NOT EXTRACTED

Episode 4: Parts 63-82 (2.2 GB estimated)
  - archive.r62 through archive.r82
  - RAR header likely embedded ❌ NOT EXTRACTED
```

### Detection Log
```
time=2025-10-06T09:08:37.194Z level=INFO msg="Archive extraction complete" 
  component=rar-processor 
  archive_number=1 
  files_extracted=1 
  parts_used=21 
  parts_remaining=63

time=2025-10-06T09:08:37.195Z level=WARN msg="Archive extraction failed, checking for RAR headers in remaining parts" 
  component=rar-processor 
  error="multi-volume RAR archive missing part 0 (.rar or .r00) - cannot process" 
  remaining_parts=63

time=2025-10-06T09:08:37.384Z level=INFO msg="Found RAR header in remaining part" 
  component=rar-processor 
  file=778c443ad551ebca4d11fa09a64134fe85a521fe.r42] 
  offset=0 
  part_index=0

time=2025-10-06T09:08:37.384Z level=ERROR msg="Found RAR headers in remaining parts - indicating multi-episode archive with embedded headers" 
  component=rar-processor 
  remaining_parts=63
  files_extracted=1

time=2025-10-06T09:08:37.384Z level=ERROR msg="This archive structure is not fully supported - only first episode can be extracted" 
  component=rar-processor

time=2025-10-06T09:08:37.384Z level=ERROR msg="Recommendation: Use SABnzbd to download and extract the complete archive" 
  component=rar-processor

time=2025-10-06T09:08:37.385Z level=ERROR msg="Item failed permanently" 
  component=importer-service 
  error="multi-episode archive with embedded RAR headers: extracted 1 of 2+ episodes - full extraction not supported"
```

### User Impact
- ✅ Episode 1 extracted but not imported (fails before metadata creation)
- ✅ Episodes 2-4 detected via RAR header scanning
- ❌ Import fails with clear diagnostic error
- 🔄 Sonarr/Radarr will try alternative sources or SABnzbd
- 📝 Clear error messages guide users to SABnzbd solution

## Best Practices

### For NZB Uploaders
To ensure maximum compatibility with streaming tools like AltMount:

**DO:**
- Use separate RAR archives per episode
- Include `.rar` or `.r00` as the first volume for each episode
- Use descriptive filenames (e.g., `ShowName.S01E01.rar`)

**DON'T:**
- Use sequential numbering across multiple episodes
- Embed RAR headers in mid-volume files
- Obfuscate filenames excessively

### For AltMount Users
When encountering sequential multi-episode archives:

1. **Check Logs:** Look for "Found RAR header" messages
2. **Partial Success:** If Episode 1 extracts, that's still valuable for immediate viewing
3. **Use SABnzbd:** For full season extraction when needed
4. **Report Structure:** Consider reporting the NZB structure to the uploader

## Technical Details

### Code Location
- **Detection Logic:** `internal/importer/rar_processor.go`
- **Function:** `checkForRarHeaders()`
- **Trigger:** When `extractSingleArchive()` fails with >10 remaining parts

### Detection Algorithm
```go
1. Extract Archive 1 successfully
2. Attempt Archive 2 → getFirstRarPart() fails (no part 0)
3. Check: len(remainingFiles) > 10?
4. If yes: Scan first 10 files for RAR signatures
5. If signature found:
   - Log: "Found RAR headers in remaining parts"
   - Log: "Recommendation: Use SABnzbd"
   - Return error: "multi-episode archive with embedded RAR headers"
6. If no signature:
   - Return error: "multi-volume RAR archive missing part 0"
```

### Performance
- **Minimal Overhead:** Only scans first 512 bytes of up to 10 files
- **Network Efficient:** Uses existing connection pool
- **Fail-Fast:** Stops scanning after first header found

## Conclusion

AltMount's RAR header detection provides intelligent handling of complex multi-episode archives. While current limitations prevent full extraction of sequential multi-episode archives, the system:

- ✅ Detects multi-episode structure via RAR header scanning
- ✅ Provides clear diagnostic information in error messages
- ✅ Fails appropriately to prevent false-positive "success"
- ✅ Guides users to SABnzbd for complete extraction
- ✅ Prevents Arr applications from marking incomplete downloads as successful
- ✅ Avoids catastrophic errors or data corruption

This represents a **fail-safe** approach that prioritizes correct behavior over partial success, ensuring media management tools like Sonarr/Radarr can properly handle alternative download strategies.

## Related Documentation
- [RAR Processing Architecture](./RAR_PROCESSING.md)
- [NZB Import Flow](./NZB_IMPORT.md)
- [Troubleshooting Guide](../5.%20Troubleshooting/common-issues.md)

## Version History
- **2025-10-06:** Initial documentation
- **Feature Added:** Commit `5035b1f` - RAR header detection for multi-episode archives

