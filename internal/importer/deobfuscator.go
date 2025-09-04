package importer

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/nzbparser"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

// Deobfuscator handles filename deobfuscation for NZB files
type Deobfuscator struct {
	poolManager pool.Manager
	log         *slog.Logger

	// Pre-compiled regex patterns for cleanup
	hexPattern32   *regexp.Regexp // 32-char hex strings
	hexPattern40   *regexp.Regexp // 40+ char hex/dot strings
	abcXyzPattern  *regexp.Regexp // abc.xyz prefix pattern
	bracketPattern *regexp.Regexp // [Word] bracket patterns
	randomPattern  *regexp.Regexp // Random character sequences
}

// NewDeobfuscator creates a new filename deobfuscator
func NewDeobfuscator(poolManager pool.Manager) *Deobfuscator {
	return &Deobfuscator{
		poolManager: poolManager,
		log:         slog.Default().With("component", "deobfuscator"),

		// Initialize regex patterns for obfuscation detection and cleanup
		hexPattern32:   regexp.MustCompile(`^[a-f0-9]{32}$`),
		hexPattern40:   regexp.MustCompile(`^[a-f0-9.]{40,}$`),
		abcXyzPattern:  regexp.MustCompile(`^abc\.xyz`),
		bracketPattern: regexp.MustCompile(`\[\w+\]`),
		randomPattern:  regexp.MustCompile(`^[a-zA-Z0-9]{20,}$`), // Long random strings
	}
}

// DeobfuscateResult contains the result of deobfuscation attempt
type DeobfuscateResult struct {
	DeobfuscatedFilename string // The cleaned/deobfuscated filename
	Method               string // How deobfuscation was achieved
	Success              bool   // Whether deobfuscation was successful
	OriginalFilename     string // The original obfuscated filename
}

// DeobfuscateFilename attempts to deobfuscate a filename using multiple strategies
func (d *Deobfuscator) DeobfuscateFilename(filename string, allFiles []nzbparser.NzbFile, currentFile nzbparser.NzbFile) *DeobfuscateResult {
	result := &DeobfuscateResult{
		OriginalFilename:     filename,
		DeobfuscatedFilename: filename,
		Success:              false,
		Method:               "none",
	}

	d.log.Debug("Attempting deobfuscation", "filename", filename)

	// Strategy 1: Try to extract filename from PAR2 files if available
	if par2Name := d.extractFromPar2Files(allFiles, filename); par2Name != "" {
		result.DeobfuscatedFilename = par2Name
		result.Method = "par2_extraction"
		result.Success = true
		d.log.Info("Deobfuscated filename using PAR2",
			"original", filename,
			"deobfuscated", par2Name)
		return result
	}

	// Strategy 2: Try to extract from yEnc headers if pool available
	if d.poolManager != nil && d.poolManager.HasPool() {
		if yencName := d.extractFromYencHeaders(currentFile); yencName != "" {
			result.DeobfuscatedFilename = yencName
			result.Method = "yenc_headers"
			result.Success = true
			d.log.Info("Deobfuscated filename using yEnc headers",
				"original", filename,
				"deobfuscated", yencName)
			return result
		}
	}

	// Strategy 3: Pattern-based cleanup of obfuscated filename
	if cleanName := d.cleanObfuscatedFilename(filename); cleanName != filename {
		result.DeobfuscatedFilename = cleanName
		result.Method = "pattern_cleanup"
		result.Success = true
		d.log.Info("Deobfuscated filename using pattern cleanup",
			"original", filename,
			"deobfuscated", cleanName)
		return result
	}

	d.log.Warn("Unable to deobfuscate filename", "filename", filename)
	return result
}

// extractFromPar2Files attempts to extract the original filename from PAR2 files
func (d *Deobfuscator) extractFromPar2Files(allFiles []nzbparser.NzbFile, targetFilename string) string {
	// Look for PAR2 files in the same NZB
	for _, file := range allFiles {
		if strings.HasSuffix(strings.ToLower(file.Filename), ".par2") {
			// Try to extract filename from PAR2 file using pool if available
			if d.poolManager != nil && d.poolManager.HasPool() {
				if filename := d.extractFilenameFromPar2(file, targetFilename); filename != "" {
					return filename
				}
			}

			// Fallback: try to infer from PAR2 filename patterns
			if filename := d.inferFromPar2Filename(file.Filename, targetFilename); filename != "" {
				return filename
			}
		}
	}

	return ""
}

// extractFilenameFromPar2 extracts filename from PAR2 file headers using streaming parsing
func (d *Deobfuscator) extractFilenameFromPar2(par2File nzbparser.NzbFile, targetFilename string) string {
	if len(par2File.Segments) == 0 {
		d.log.Debug("PAR2 file has no segments", "par2_file", par2File.Filename)
		return ""
	}

	cp, err := d.poolManager.GetPool()
	if err != nil {
		d.log.Debug("No connection pool available for PAR2 extraction", "error", err)
		return ""
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Try first segment for PAR2 content
	firstSegment := par2File.Segments[0]
	r, err := cp.BodyReader(ctx, firstSegment.ID, par2File.Groups)
	if err != nil {
		d.log.Debug("Failed to get body reader for PAR2 extraction", "error", err)
		return ""
	}
	defer r.Close()

	d.log.Debug("Attempting to parse PAR2 file content",
		"par2_file", par2File.Filename,
		"target", targetFilename)

	// Parse PAR2 packets and look for file description packets
	return d.streamParsePAR2(r, targetFilename)
}

// inferFromPar2Filename tries to infer original filename from PAR2 naming patterns
func (d *Deobfuscator) inferFromPar2Filename(par2Filename, targetFilename string) string {
	// Many PAR2 files are named like: "OriginalName.par2" or "OriginalName.vol01+02.par2"
	// Try to extract the base name

	// Remove .par2 extension and volume info
	baseName := par2Filename

	// Remove .par2 suffix (case insensitive)
	if strings.HasSuffix(strings.ToLower(baseName), ".par2") {
		baseName = baseName[:len(baseName)-5]
	}

	// Remove volume patterns like .vol01+02 (case insensitive)
	volPattern := regexp.MustCompile(`(?i)\.vol\d+\+\d+$`)
	baseName = volPattern.ReplaceAllString(baseName, "")

	// If the base name looks like a real filename and differs from target, use it
	if len(baseName) > 3 && !IsProbablyObfuscated(baseName) && !strings.EqualFold(baseName, strings.TrimSuffix(targetFilename, filepath.Ext(targetFilename))) {
		// Try to preserve original casing and add common extensions
		return d.improveFilename(baseName, targetFilename)
	}

	return ""
}

// extractFromYencHeaders attempts to extract filename from yEnc headers
func (d *Deobfuscator) extractFromYencHeaders(file nzbparser.NzbFile) string {
	if len(file.Segments) == 0 {
		return ""
	}

	cp, err := d.poolManager.GetPool()
	if err != nil {
		d.log.Debug("No connection pool available for yEnc extraction", "error", err)
		return ""
	}

	// Create context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Try first segment for yEnc headers
	firstSegment := file.Segments[0]
	r, err := cp.BodyReader(ctx, firstSegment.ID, file.Groups)
	if err != nil {
		d.log.Debug("Failed to get body reader for yEnc extraction", "error", err)
		return ""
	}
	defer r.Close()

	// Get yenc headers
	h, err := r.GetYencHeaders()
	if err != nil {
		d.log.Debug("Failed to get yEnc headers", "error", err)
		return ""
	}

	// Check if yEnc filename is different and appears less obfuscated
	if h.FileName != "" && h.FileName != file.Filename {
		if !IsProbablyObfuscated(h.FileName) {
			return h.FileName
		}
	}

	return ""
}

// cleanObfuscatedFilename applies pattern-based cleanup to remove obfuscation artifacts
func (d *Deobfuscator) cleanObfuscatedFilename(filename string) string {
	// Extract file extension first
	ext := ""
	name := filename
	if dotIndex := strings.LastIndex(filename, "."); dotIndex > 0 {
		ext = filename[dotIndex:]
		name = filename[:dotIndex]
	}

	originalName := name
	cleaned := name

	// Remove abc.xyz prefix
	if d.abcXyzPattern.MatchString(cleaned) {
		cleaned = strings.TrimPrefix(cleaned, "abc.xyz")
		cleaned = strings.TrimPrefix(cleaned, ".")
	}

	// Remove bracket patterns [Word] - often used in obfuscation
	cleaned = d.bracketPattern.ReplaceAllString(cleaned, "")

	// Clean up common obfuscation artifacts - fix multiple dots first
	cleaned = regexp.MustCompile(`\.{2,}`).ReplaceAllString(cleaned, ".")
	cleaned = strings.Trim(cleaned, ".-_")

	// If the cleaned name is too short, try other approaches
	if len(cleaned) < 3 {
		// Try to extract meaningful parts from the original filename
		cleaned = d.extractMeaningfulParts(originalName)
	}

	// Reconstruct filename with extension if we made improvements
	if cleaned != "" && cleaned != originalName {
		return cleaned + ext
	}

	return filename
}

// extractMeaningfulParts attempts to extract meaningful parts from obfuscated filenames
func (d *Deobfuscator) extractMeaningfulParts(filename string) string {
	// Look for patterns that might indicate real content
	// This is a simplified approach - real deobfuscation would use more sophisticated methods

	parts := strings.FieldsFunc(filename, func(r rune) bool {
		return r == '.' || r == '-' || r == '_' || r == ' '
	})

	var meaningfulParts []string
	for _, part := range parts {
		// Skip pure hex strings and very short parts
		if len(part) < 3 {
			continue
		}
		if d.hexPattern32.MatchString(part) {
			continue
		}
		if d.randomPattern.MatchString(part) && len(part) > 15 {
			continue
		}

		// Keep parts that look like they might contain meaningful content
		meaningfulParts = append(meaningfulParts, part)
	}

	if len(meaningfulParts) > 0 {
		return strings.Join(meaningfulParts, ".")
	}

	return filename
}

// improveFilename attempts to improve a basic filename by adding proper casing and extensions
func (d *Deobfuscator) improveFilename(baseName, originalFilename string) string {
	// Extract extension from original filename
	ext := ""
	if dotIndex := strings.LastIndex(originalFilename, "."); dotIndex > 0 {
		ext = originalFilename[dotIndex:]
	}

	// Apply basic case improvements
	caser := cases.Title(language.English)
	improved := caser.String(strings.ToLower(baseName))

	// Add extension if not present
	if ext != "" && !strings.HasSuffix(improved, ext) {
		improved += ext
	}

	return improved
}

// IsDeobfuscationWorthwhile checks if deobfuscation is worth attempting
func (d *Deobfuscator) IsDeobfuscationWorthwhile(filename string, allFiles []nzbparser.NzbFile) bool {
	// Don't attempt deobfuscation if filename already looks good
	if !IsProbablyObfuscated(filename) {
		return false
	}

	// More worthwhile if we have PAR2 files available
	for _, file := range allFiles {
		if strings.HasSuffix(strings.ToLower(file.Filename), ".par2") {
			return true
		}
	}

	// Worthwhile if we have connection pool for yEnc headers
	if d.poolManager != nil && d.poolManager.HasPool() {
		return true
	}

	// Always worth trying basic pattern cleanup
	return true
}

// PAR2PacketHeader represents the header of a PAR2 packet
type PAR2PacketHeader struct {
	Magic      [8]byte  // "PAR2\0PKT"
	Length     uint64   // Total packet length including header
	MD5Hash    [16]byte // MD5 hash of packet
	RecoveryID [16]byte // Recovery Set ID
	Type       [16]byte // Packet type
}

// PAR2FileDesc represents a file description packet content
type PAR2FileDesc struct {
	FileID     [16]byte // Unique file identifier
	FileMD5    [16]byte // MD5 hash of entire file
	FirstMD5   [16]byte // MD5 hash of first 16KB
	FileLength uint64   // File length in bytes
	Filename   string   // Original filename (variable length)
}

// parsePAR2Header reads and validates a PAR2 packet header from a reader
func (d *Deobfuscator) parsePAR2Header(r io.Reader) (*PAR2PacketHeader, error) {
	header := &PAR2PacketHeader{}

	// Read the header (8 + 8 + 16 + 16 + 16 = 64 bytes)
	if err := binary.Read(r, binary.LittleEndian, header); err != nil {
		return nil, fmt.Errorf("failed to read PAR2 header: %w", err)
	}

	// Validate magic signature
	expectedMagic := [8]byte{'P', 'A', 'R', '2', 0, 'P', 'K', 'T'}
	if header.Magic != expectedMagic {
		return nil, fmt.Errorf("invalid PAR2 magic signature")
	}

	return header, nil
}

// parseFileDescPacket reads and parses a file description packet
func (d *Deobfuscator) parseFileDescPacket(r io.Reader, packetLength uint64) (*PAR2FileDesc, error) {
	// Calculate remaining bytes after header (64 bytes)
	contentLength := packetLength - 64
	if contentLength < 56 { // Minimum: 16 + 16 + 16 + 8 = 56 bytes for fixed fields
		return nil, fmt.Errorf("file description packet too small: %d bytes", contentLength)
	}

	desc := &PAR2FileDesc{}

	// Read fixed fields (56 bytes total)
	if err := binary.Read(r, binary.LittleEndian, &desc.FileID); err != nil {
		return nil, fmt.Errorf("failed to read FileID: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.FileMD5); err != nil {
		return nil, fmt.Errorf("failed to read FileMD5: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.FirstMD5); err != nil {
		return nil, fmt.Errorf("failed to read FirstMD5: %w", err)
	}
	if err := binary.Read(r, binary.LittleEndian, &desc.FileLength); err != nil {
		return nil, fmt.Errorf("failed to read FileLength: %w", err)
	}

	// Read filename (remaining bytes, not null-terminated)
	filenameLength := contentLength - 56
	if filenameLength > 0 {
		filenameBytes := make([]byte, filenameLength)
		if _, err := io.ReadFull(r, filenameBytes); err != nil {
			return nil, fmt.Errorf("failed to read filename: %w", err)
		}

		// Remove padding bytes (PAR2 uses 4-byte alignment)
		// Find the actual end of the filename by looking for null bytes or non-printable chars
		actualLength := filenameLength
		for i := len(filenameBytes) - 1; i >= 0; i-- {
			if filenameBytes[i] == 0 || filenameBytes[i] < 32 {
				actualLength = uint64(i)
			} else {
				break
			}
		}

		desc.Filename = string(filenameBytes[:actualLength])
	}

	return desc, nil
}

// skipBytes skips the specified number of bytes in the reader
func (d *Deobfuscator) skipBytes(r io.Reader, n uint64) error {
	if n == 0 {
		return nil
	}

	// Use io.CopyN with a discard writer to skip bytes efficiently
	_, err := io.CopyN(io.Discard, r, int64(n))
	return err
}

// streamParsePAR2 streams through PAR2 content looking for file description packets
func (d *Deobfuscator) streamParsePAR2(r io.Reader, targetFilename string) string {
	maxPackets := 50 // Limit the number of packets to process
	packetCount := 0

	for packetCount < maxPackets {
		// Parse packet header
		header, err := d.parsePAR2Header(r)
		if err != nil {
			d.log.Debug("Failed to parse PAR2 header or reached end", "error", err)
			break
		}

		packetCount++
		d.log.Debug("Found PAR2 packet",
			"type", string(header.Type[:]),
			"length", header.Length)

		// Check if this is a file description packet
		expectedFileDescType := [16]byte{'P', 'A', 'R', ' ', '2', '.', '0', 0, 'F', 'i', 'l', 'e', 'D', 'e', 's', 'c'}
		if header.Type == expectedFileDescType {
			d.log.Debug("Processing file description packet", "length", header.Length)

			// Parse file description packet
			fileDesc, err := d.parseFileDescPacket(r, header.Length)
			if err != nil {
				d.log.Debug("Failed to parse file description packet", "error", err)
				// Skip to next packet
				continue
			}

			d.log.Debug("Found filename in PAR2",
				"filename", fileDesc.Filename,
				"file_length", fileDesc.FileLength)

			// Check if this filename looks less obfuscated than target
			if fileDesc.Filename != "" &&
				fileDesc.Filename != targetFilename &&
				!IsProbablyObfuscated(fileDesc.Filename) {
				d.log.Info("Found better filename in PAR2",
					"original", targetFilename,
					"par2_filename", fileDesc.Filename)
				return fileDesc.Filename
			}

			// Even if obfuscated, if it's different, it might be useful
			if fileDesc.Filename != "" && fileDesc.Filename != targetFilename {
				d.log.Debug("Found alternative filename in PAR2 (may still be obfuscated)",
					"filename", fileDesc.Filename)
				// Continue looking for better options but remember this one
			}
		} else {
			// Skip non-file-description packets by reading the remaining content
			remainingBytes := header.Length - 64 // Header is 64 bytes
			if remainingBytes > 0 {
				if err := d.skipBytes(r, remainingBytes); err != nil {
					d.log.Debug("Failed to skip packet content", "error", err)
					break
				}
			}
		}
	}

	d.log.Debug("Completed PAR2 parsing", "packets_processed", packetCount)
	return ""
}

// isProbablyObfuscated returns true if the filename (or full path) appears to be
// obfuscated, following heuristics translated from the provided Python logic.
// Default outcome is true (assume obfuscated) unless clear patterns indicate otherwise.
//
// Python reference logic summary:
// 1. Definitely obfuscated patterns:
//   - Exactly 32 lowercase hex chars
//   - 40+ chars consisting only of lowercase hex or dots
//   - Contains a 30+ lowercase hex substring AND at least two bracketed words [Word]
//   - Starts with "abc.xyz"
//
// 2. Clearly NOT obfuscated patterns:
//   - At least 2 uppercase, 2 lowercase, and 1 space/dot/underscore
//   - At least 3 space/dot/underscore characters
//   - Letters (upper+lower) >= 4, digits >= 4, and at least 1 space/dot/underscore
//   - Starts with capital, has >2 lowercase, and upper/lower ratio <= 0.25
//
// 3. Otherwise: obfuscated.
// IsProbablyObfuscated returns true if the provided filename/path appears obfuscated.
// See detailed heuristic description above.
func IsProbablyObfuscated(input string) bool {
	logger := slog.Default()

	// Extract filename then its basename without extension
	filename := filepath.Base(input)
	ext := filepath.Ext(filename)
	filebasename := strings.TrimSuffix(filename, ext)
	if filebasename == "" { // empty name -> treat as obfuscated default
		logger.Debug("obfuscation check: empty basename -> default obfuscated", "input", input)
		return true
	}
	logger.Debug("obfuscation check: analyzing", "basename", filebasename)

	// Compile (or reuse) regexes (precompiled at first call via package-level vars could optimize; kept inline for clarity)
	if matched, _ := regexp.MatchString(`^[a-f0-9]{32}$`, filebasename); matched {
		logger.Debug("obfuscation check: exactly 32 hex digits -> obfuscated", "basename", filebasename)
		return true
	}

	if matched, _ := regexp.MatchString(`^[a-f0-9.]{40,}$`, filebasename); matched {
		logger.Debug("obfuscation check: 40+ hex/dot chars -> obfuscated", "basename", filebasename)
		return true
	}

	// Contains 30+ hex substring AND at least two [Word] occurrences
	has30Hex, _ := regexp.MatchString(`[a-f0-9]{30}`, filebasename)
	bracketWords := regexp.MustCompile(`\[\w+\]`).FindAllString(filebasename, -1)
	if has30Hex && len(bracketWords) >= 2 {
		logger.Debug("obfuscation check: 30+ hex plus 2+ [Word] -> obfuscated", "basename", filebasename)
		return true
	}

	if strings.HasPrefix(filebasename, "abc.xyz") { // ^abc\.xyz
		logger.Debug("obfuscation check: starts with abc.xyz -> obfuscated", "basename", filebasename)
		return true
	}

	// Counts for non-obfuscated heuristics
	var digits, uppers, lowers, spacesDots int
	for _, r := range filebasename {
		switch {
		case unicode.IsDigit(r):
			digits++
		case unicode.IsUpper(r):
			uppers++
		case unicode.IsLower(r):
			lowers++
		}
		if r == ' ' || r == '.' || r == '_' { // space-like set
			spacesDots++
		}
	}

	// Not obfuscated heuristics
	if uppers >= 2 && lowers >= 2 && spacesDots >= 1 {
		logger.Debug("obfuscation check: pattern (>=2 upper, >=2 lower, >=1 space/dot/underscore) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	if spacesDots >= 3 {
		logger.Debug("obfuscation check: pattern (spaces/dots/underscores >=3) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	if (uppers+lowers) >= 4 && digits >= 4 && spacesDots >= 1 {
		logger.Debug("obfuscation check: pattern (letters>=4, digits>=4, space-like>=1) -> NOT obfuscated", "basename", filebasename)
		return false
	}
	// Starts with capital, mostly lowercase
	firstRune, _ := utf8.DecodeRuneInString(filebasename)
	if unicode.IsUpper(firstRune) && lowers > 2 && (lowers > 0) && float64(uppers)/float64(lowers) <= 0.25 {
		logger.Debug("obfuscation check: pattern (Capital start, mostly lowercase) -> NOT obfuscated", "basename", filebasename)
		return false
	}

	logger.Debug("obfuscation check: default -> obfuscated", "basename", filebasename)
	return true
}
