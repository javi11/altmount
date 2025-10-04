# Smurfs NZB Fix - Part 2: RAR First Part Detection

## Status Update

✅ **FIXED:** RAR Detection Logic (Part 1)
- The Smurfs NZB is now correctly detected as `type=rar_archive` (12 files)
- Multi-source RAR filename checking is working

❌ **NEW ISSUE:** RAR First Part Detection (Part 2)  
- Error: `"no valid first RAR part found in archive"`
- The RAR processor can't identify which file is the first part to begin extraction

## Current Behavior

From your logs:
```
time=2025-10-04T08:47:32.542Z level=INFO msg="Processing file" 
  type=rar_archive total_size=486969256 files=12
  
time=2025-10-04T08:47:32.543Z level=INFO msg="Processing RAR archive with content analysis"
  archive=The.Smurfs.2021.S01E13.DUTCH.1080p.WEB.h264-NLKIDS-xpost parts=9
  
time=2025-10-04T08:47:32.543Z level=ERROR msg="Failed to analyze RAR archive content"
  error="no valid first RAR part found in archive"
```

## Root Cause Analysis

The `getFirstRarPart` function in `rar_processor.go` filters for files with `part == 0`:
```go
// Only consider files that are actually first parts (part 0)
if part != 0 {
    continue
}
```

If no files are recognized as "part 0", it fails with this error.

### Files in the Archive

From the logs, the Smurfs archive contains:
- `.nfo` file (not a RAR part) ✅ Correctly excluded
- `-sample.mkv` (not a RAR part) ✅ Correctly excluded
- `.sfv` file (not a RAR part) ✅ Correctly excluded
- **9 RAR parts:** `.rar`, `.r00`, `.r01`, `.r02`, `.r03`, `.r04`, `.r05`, `.r06`, `.r07`

### Expected Behavior

The `parseRarFilename` function should recognize:
- `*.rar` → part 0 (highest priority)
- `*.r00` → part 0 (next priority)
- `*.r01` → part 1
- `*.r02` → part 2
- etc.

## What I've Added

### Enhanced Debug Logging

Added comprehensive logging to `internal/importer/rar_processor.go`:

```go
rh.log.Debug("Analyzing RAR files for first part detection",
    "total_files", len(rarFileNames),
    "filenames", rarFileNames)

for _, filename := range rarFileNames {
    base, part := rh.parseRarFilename(filename)
    
    rh.log.Debug("Parsed RAR filename",
        "filename", filename,
        "base", base,
        "part", part)
    // ...
}

if len(candidates) == 0 {
    rh.log.Error("No valid first RAR part found",
        "total_files", len(rarFileNames),
        "files_checked", rarFileNames)
    // ...
}
```

## Next Steps

### 1. Rebuild Altmount

You'll need to rebuild with the new changes:
```bash
# Navigate to project directory
cd /Users/rolandbo@backbase.com/Documents/Coding\ Projects/Altmount/altmount

# Build the binary (adjust command as needed for your setup)
go build -o altmount ./cmd/altmount

# Or if you use make:
make build
```

### 2. Stop Current Instance

Stop the currently running altmount process.

### 3. Start with Debug Logging

```bash
# Start altmount with debug level logging
./altmount serve --log-level=debug
```

### 4. Re-Upload the Smurfs NZB

Upload the Smurfs NZB again through the web UI.

### 5. Capture the New Debug Logs

Look for these new log messages:
```
level=DEBUG msg="Analyzing RAR files for first part detection"
level=DEBUG msg="Parsed RAR filename"
```

These will show:
- The actual filenames after `renameRarFilesAndSort` 
- What base name and part number each file is parsed as
- Why no files are being recognized as "part 0"

## Potential Issues to Investigate

Based on the code analysis, possible causes include:

### 1. Filename Renaming Issue
The `renameRarFilesAndSort` function extracts a base name from the first RAR file and renames all parts. If this produces unexpected filenames, the regex patterns might not match.

### 2. Regex Pattern Mismatch
The patterns in `rar.go`:
```go
rPattern = regexp.MustCompile(`^(.+)\.r(\d+)$`)       // Matches: basename.r00
partPattern = regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`)  // Matches: basename.part01.rar
```

If the renamed filenames don't match these exact patterns, they won't be recognized.

### 3. Special Characters in Filenames
The filenames contain brackets and special prefixes:
```
[PRiVATE]-[WtFnZb]-[the.smurfs.2021.s01e13.dutch.1080p.web.h264-nlkids.r00]
```

If there are unexpected characters or encoding issues, the regex might not match.

### 4. yEnc vs NZB Filename Confusion
The yEnc filenames are SHA256 hashes:
```
yenc_filename=284d922bd31143a979f3fce65c4f4eb3ae1736ce682f9d5960ebba5c1f998367.r00
```

If the processor uses these instead of the NZB filenames, the base name extraction could fail.

## Debug Log Format

When you capture the logs, look for patterns like this:

```
level=DEBUG msg="Analyzing RAR files for first part detection" total_files=9 filenames=[...]
level=DEBUG msg="Parsed RAR filename" filename="<name>.rar" base="<base>" part=0
level=DEBUG msg="Parsed RAR filename" filename="<name>.r00" base="<base>" part=0
level=DEBUG msg="Parsed RAR filename" filename="<name>.r01" base="<base>" part=1
...
```

If all files show `part=1` or higher (never `part=0`), that confirms the parsing issue.

## Files Modified

- `internal/importer/parser.go` (Part 1 - ✅ Working)
  - Fixed yEnc header fetching to be non-fatal
  - Enhanced RAR detection to check all filename sources
  
- `internal/importer/rar_processor.go` (Part 2 - ⏳ Debugging)
  - Added comprehensive debug logging to trace filename parsing
  - Lines 152-154: Log all filenames before parsing
  - Lines 159-162: Log each parsed filename with base and part number
  - Lines 181-183: Log error details when no first part found

## What to Share Next

Please share the output containing these debug lines after rebuilding and re-uploading the Smurfs NZB. This will reveal exactly why the RAR processor can't find the first part, and I can provide a targeted fix.

---

**Note:** The first fix (RAR detection) is working perfectly! Now we just need to fix the second step (first part identification) to complete the end-to-end flow.

