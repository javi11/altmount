package importer

import (
	"bufio"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/gabriel-vasile/mimetype"
)

// This file provides helpers translated from https://github.com/sabnzbd/sabnzbd/blob/develop/sabnzbd/utils/file_extension.py for detecting
// popular/likely file extensions, adapted to Go.

// popularExt and downloadExt are combined (unique) and dot-prefixed.
var popularExt = []string{
	"3g2", "3gp", "7z", "aac", "abw", "ai", "aif", "apk", "arc", "arj",
	"asp", "aspx", "avi", "azw", "bak", "bat", "bin", "bmp", "bz", "bz2",
	"c", "cab", "cda", "cer", "cfg", "cfm", "cgi", "class", "com", "cpl",
	"cpp", "cs", "csh", "css", "csv", "cur", "dat", "db", "dbf", "deb",
	"dll", "dmg", "dmp", "doc", "docx", "drv", "email", "eml", "emlx",
	"eot", "epub", "exe", "flv", "fnt", "fon", "gadget", "gif", "gz",
	"h", "h264", "htm", "html", "icns", "ico", "ics", "ini", "iso", "jar",
	"java", "jpeg", "jpg", "js", "json", "jsonld", "jsp", "key", "lnk",
	"log", "m4v", "mdb", "mid", "midi", "mjs", "mkv", "mov", "mp3", "mp4",
	"mpa", "mpeg", "mpg", "mpkg", "msg", "msi", "odp", "ods", "odt",
	"oft", "oga", "ogg", "ogv", "ogx", "opus", "ost", "otf", "part", "pdf",
	"php", "pkg", "pl", "png", "pps", "ppt", "pptx", "ps", "psd", "pst",
	"py", "rar", "rm", "rpm", "rss", "rtf", "sav", "sh", "sql", "svg",
	"swf", "swift", "sys", "tar", "tex", "tif", "tiff", "tmp", "toast",
	"ts", "ttf", "txt", "vb", "vcd", "vcf", "vob", "vsd", "wav", "weba",
	"webm", "webp", "wma", "wmv", "woff", "woff2", "wpd", "wpl", "wsf",
	"xhtml", "xls", "xlsm", "xlsx", "xml", "xul", "z", "zip",
}

var downloadExt = []string{
	"ass", "avi", "azw3", "bat", "bdmv", "bin", "bup", "cbr", "cbz",
	"clpi", "crx", "db", "diz", "djvu", "docx", "epub", "exe", "flac",
	"gif", "gz", "htm", "html", "icns", "ico", "idx", "ifo", "img", "inf",
	"info", "ini", "iso", "jpg", "log", "m2ts", "m3u", "m4a", "m4b", "mkv",
	"mobi", "mp3", "mp4", "mpls", "nfo", "nib", "nzb", "otf", "par2",
	"part", "pdf", "pem", "plist", "png", "py", "rar", "releaseinfo",
	"rev", "sfv", "sh", "srr", "srs", "srt", "ssa", "strings", "sub",
	"sup", "sys", "tif", "ttf", "txt", "url", "vob", "wmv", "xpi",
}

var allExt = func() []string {
	// Build unique set and dot-prefix
	uniq := map[string]struct{}{}
	add := func(list []string) {
		for _, e := range list {
			e = strings.ToLower(strings.TrimPrefix(e, "."))
			if e == "" {
				continue
			}
			uniq["."+e] = struct{}{}
		}
	}
	add(popularExt)
	add(downloadExt)
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}()

// AllExtensions returns the combined list of known extensions (dot-prefixed).
// If you need to add user-defined extensions, pass them to AllExtensionsWith.
func AllExtensions() []string { return allExt }

// AllExtensionsWith returns combined list plus user-defined dot- or non-dot-prefixed extensions.
func AllExtensionsWith(extra []string) []string {
	if len(extra) == 0 {
		return allExt
	}
	set := map[string]struct{}{}
	for _, e := range allExt {
		set[e] = struct{}{}
	}
	for _, e := range extra {
		e = strings.ToLower(e)
		if e == "" {
			continue
		}
		if !strings.HasPrefix(e, ".") {
			e = "." + e
		}
		set[e] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// HasPopularExtension reports whether file_path has a popular extension (case-insensitive)
// or matches known RAR patterns (e.g., .rar, .r00, .partXX.rar).
func HasPopularExtension(filePath string) bool {
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == "" {
		return false
	}
	// direct membership check
	for _, e := range allExt {
		if e == ext {
			return true
		}
	}
	// Fallback to the package's RAR detector on the basename
	base := filepath.Base(filePath)
	return rarPattern.MatchString(strings.ToLower(base))
}

// AllPossibleExtensions attempts to detect the file's extension(s).
// Unlike Python's puremagic (which may return multiple candidates), we
// typically have one strong match via signature-based detection.
// The returned extensions are dot-prefixed and lowercase.
func AllPossibleExtensions(filePath string) []string {
	// Try MIME-based detection
	if mt, err := mimetype.DetectFile(filePath); err == nil && mt != nil {
		if ext := strings.ToLower(mt.Extension()); ext != "" {
			return []string{ext}
		}
	}
	return nil
}

// WhatIsMostLikelyExtension returns the most likely extension (dot-prefixed) for file_path.
// Logic mirrors the Python version:
// 1) If the start of the file is valid UTF-8 text, check for NZB clues, else return .txt
// 2) Otherwise, use signature detection and prefer a popular extension if it matches
// 3) Fallback to the first detected extension or empty string if none.
func WhatIsMostLikelyExtension(filePath string) string {
	// 1) Quick text/NZB check on the first ~200 bytes
	if ext, ok := sniffTextOrNZB(filePath, 200); ok {
		return ext
	}

	// 2) signature detection
	candidates := AllPossibleExtensions(filePath)
	if len(candidates) == 0 {
		return ""
	}
	// Prefer popular extension
	all := allExt
	for _, cand := range candidates {
		if slices.Contains(all, strings.ToLower(cand)) {
			return strings.ToLower(cand)
		}
	}
	// 3) fallback to first
	return strings.ToLower(candidates[0])
}

// sniffTextOrNZB reads up to n bytes and checks if it's valid UTF-8 text and
// whether it contains NZB markers. Returns (ext, true) when determined.
func sniffTextOrNZB(filePath string, n int) (string, bool) {
	f, err := os.Open(filePath)
	if err != nil {
		return "", false
	}
	defer f.Close()

	r := bufio.NewReader(f)
	buf, _ := r.Peek(n)
	// If valid UTF-8, treat as text
	if !isLikelyUTF8(buf) {
		return "", false
	}
	lower := strings.ToLower(string(buf))
	if strings.Contains(lower, "!doctype nzb public") || strings.Contains(lower, "<nzb xmlns=") {
		return ".nzb", true
	}
	return ".txt", true
}

// isLikelyUTF8 returns true if b looks like UTF-8 (simple heuristic)
func isLikelyUTF8(b []byte) bool {
	// Use Go's decoder by converting to string and back
	// If it contains NUL bytes or replacement characters after round-trip,
	// consider it unlikely text.
	s := string(b)
	// If the conversion replaced invalid sequences, the resulting bytes differ
	if !slices.Equal([]byte(s), b) {
		return false
	}
	if strings.IndexByte(s, '\x00') >= 0 {
		return false
	}
	return true
}
