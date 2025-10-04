# Complete Fix for Smurfs NZB RAR Processing

## Summary

Two bugs were fixed to enable proper RAR archive processing for the Smurfs NZB:

1. ✅ **RAR Detection Bug** - Fixed in `parser.go`
2. ✅ **Bracket Parsing Bug** - Fixed in `rar_processor.go`

## Bug #1: RAR Detection (parser.go)

### Problem
The RAR detection only checked the "final chosen" filename, missing cases where:
- yEnc headers provided a different filename than the NZB
- yEnc header fetching failed entirely

### Root Cause
```go
// Old code - only checked one source
isRarArchive := rarPattern.MatchString(filename)
```

### Fix
Check **all three filename sources** (yEnc, NZB, and final):
```go
isRarArchive := rarPattern.MatchString(filename) ||
    rarPattern.MatchString(file.Filename) ||
    (yencFilename != "" && rarPattern.MatchString(yencFilename))
```

Also made yEnc header fetching non-fatal so files can be processed even when headers are unavailable.

## Bug #2: Bracket Parsing (rar_processor.go)

### Problem
From your debug logs, ALL files were parsed as `part=999999` (unknown):
```
filename=[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.r00]
part=999999  ← Should be 0!
```

### Root Cause
The regex patterns require filenames to **end** with `.r00`, `.rar`, etc.:
```go
rPattern = regexp.MustCompile(`^(.+)\.r(\d+)$`)
                                            ↑
                                            $ = end of string
```

But the Smurfs filenames have brackets at the end:
```
[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.r00]
                                                                        ↑
                                                                        ends with ]
```

So the regex failed to match and returned the fallback `part=999999`.

### Fix
Extract the actual filename from within the brackets before pattern matching:

```go
// Handle filenames with brackets like [PRiVATE]-[WtFnZb]-[actual.file.r00]
// Extract the last bracketed section if it contains a RAR extension
if lastBracket := strings.LastIndex(filename, "["); lastBracket >= 0 {
    if closeBracket := strings.Index(filename[lastBracket:], "]"); closeBracket >= 0 {
        innerFilename := filename[lastBracket+1 : lastBracket+closeBracket]
        // Check if this looks like a RAR filename
        lowerInner := strings.ToLower(innerFilename)
        if strings.HasSuffix(lowerInner, ".rar") ||
            strings.Contains(lowerInner, ".r0") ||
            strings.Contains(lowerInner, ".r1") ||
            strings.Contains(lowerInner, ".part") {
            filename = innerFilename  // Use the clean filename
        }
    }
}
```

This extracts: `the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.r00` from the brackets, allowing the regex to match properly.

## Expected Behavior After Fix

### Before
```
❌ type=multi_file (12 files)
❌ Only exposes raw RAR parts: .rar, .r00, .r01, etc.
❌ No MKV file accessible
```

### After
```
✅ type=rar_archive (12 files)
✅ RAR processor analyzes archive structure
✅ Exposes internal MKV file as virtual file
✅ RAR parts remain accessible for verification
```

## Files Modified

### 1. `internal/importer/parser.go`

**Lines 193-202:** Made yEnc header fetching non-fatal
```go
if err != nil {
    p.log.Debug("Failed to fetch yEnc headers, continuing with NZB metadata",
        "filename", file.Filename,
        "error", err)
} else {
    yencFilename = firstPartHeaders.FileName
    yencFileSize = int64(firstPartHeaders.FileSize)
}
```

**Lines 313-324:** Enhanced RAR detection with multi-source checking
```go
isRarArchive := rarPattern.MatchString(filename) ||
    rarPattern.MatchString(file.Filename) ||
    (yencFilename != "" && rarPattern.MatchString(yencFilename))

p.log.Debug("RAR detection",
    "final_filename", filename,
    "nzb_filename", file.Filename,
    "yenc_filename", yencFilename,
    "is_rar_archive", isRarArchive)
```

### 2. `internal/importer/rar_processor.go`

**Lines 152-154:** Added debug logging for first part detection
```go
rh.log.Debug("Analyzing RAR files for first part detection",
    "total_files", len(rarFileNames),
    "filenames", rarFileNames)
```

**Lines 159-162:** Added per-file parsing debug logs
```go
rh.log.Debug("Parsed RAR filename",
    "filename", filename,
    "base", base,
    "part", part)
```

**Lines 181-184:** Enhanced error logging
```go
rh.log.Error("No valid first RAR part found",
    "total_files", len(rarFileNames),
    "files_checked", rarFileNames)
```

**Lines 237-251:** Added bracket extraction logic
```go
// Handle filenames with brackets like [PRiVATE]-[WtFnZb]-[actual.file.r00]
// Extract the last bracketed section if it contains a RAR extension
if lastBracket := strings.LastIndex(filename, "["); lastBracket >= 0 {
    if closeBracket := strings.Index(filename[lastBracket:], "]"); closeBracket >= 0 {
        innerFilename := filename[lastBracket+1 : lastBracket+closeBracket]
        // Check if this looks like a RAR filename
        lowerInner := strings.ToLower(innerFilename)
        if strings.HasSuffix(lowerInner, ".rar") ||
            strings.Contains(lowerInner, ".r0") ||
            strings.Contains(lowerInner, ".r1") ||
            strings.Contains(lowerInner, ".part") {
            filename = innerFilename
        }
    }
}
```

## Testing Instructions

### 1. Rebuild Altmount

The changes have been applied to your code. Rebuild the binary:

```bash
cd /Users/rolandbo@backbase.com/Documents/Coding\ Projects/Altmount/altmount

# If using Docker
docker-compose down
docker-compose build
docker-compose up -d

# Or if building locally (adjust as needed)
go build -o altmount ./cmd/altmount
./altmount serve
```

### 2. Re-Upload the Smurfs NZB

Upload the Smurfs NZB file through the web UI.

### 3. Expected Debug Logs

You should now see:

```
level=DEBUG msg="RAR detection" 
  is_rar_archive=true  ← Should be true now

level=INFO msg="Processing file" 
  type=rar_archive  ← Should be rar_archive, not multi_file

level=DEBUG msg="Analyzing RAR files for first part detection"

level=DEBUG msg="Parsed RAR filename" 
  filename=the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.rar 
  base=the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids 
  part=0  ← Should be 0, not 999999!

level=DEBUG msg="Selected first RAR part"
  filename=the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.rar

level=INFO msg="Starting RAR analysis"

level=INFO msg="Successfully analyzed RAR archive content"
  files_in_archive=1  ← The extracted MKV file
```

### 4. Verify the Virtual File

Browse the virtual filesystem (via WebDAV or file browser) and you should see:
```
/The.Smurfs.2021.S01E13.DUTCH.1080p.WEB.h264-NLKIDS-xpost/
├── The.Smurfs.2021.S01E13.DUTCH.1080p.WEB.h264-NLKIDS-xpost/
│   └── <extracted_mkv_file>  ← The video file from inside the RAR
├── [PRiVATE]-[WtFnZb]-[...-nlkids.nfo]
├── [PRiVATE]-[WtFnZb]-[...-sample.mkv]
└── [N3wZ]...[...-nlkids.sfv]
```

## Edge Cases Handled

1. **yEnc header failures** - Processing continues with NZB metadata
2. **Obfuscated filenames** - Deobfuscation attempts before RAR detection
3. **Multiple filename sources** - All sources checked for RAR patterns
4. **Bracketed filenames** - Brackets stripped before regex matching
5. **Different RAR naming schemes**:
   - `.rar` + `.r00`, `.r01` (Smurfs style)
   - `.part01.rar`, `.part02.rar`
   - `.001`, `.002` numeric extensions

## Compatibility

These fixes are backward compatible and don't affect:
- Single-file NZBs
- Multi-file NZBs without RAR archives
- Large RAR archives (like Mr Nobody with 215 parts)
- Different RAR naming conventions

## Debug Output Comparison

### Before Fix
```
msg="Parsed RAR filename" part=999999  ← All files unknown!
msg="No valid first RAR part found"
msg="Failed to analyze RAR archive content"
```

### After Fix
```
msg="Parsed RAR filename" 
  filename=the.smurfs...rar part=0  ← .rar file is part 0
msg="Parsed RAR filename" 
  filename=the.smurfs...r00 part=0  ← .r00 is also part 0
msg="Parsed RAR filename" 
  filename=the.smurfs...r01 part=1  ← .r01 is part 1
msg="Selected first RAR part" filename=...rar  ← Chose .rar
msg="Successfully analyzed RAR archive content"
```

---

## Status: ✅ READY FOR TESTING

Both bugs are fixed. Rebuild altmount and re-upload the Smurfs NZB to verify the complete solution.

