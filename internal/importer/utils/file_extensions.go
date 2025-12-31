package utils

import (
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// This file provides helpers translated from https://github.com/sabnzbd/sabnzbd/blob/develop/sabnzbd/utils/file_extension.py for detecting
// popular/likely file extensions, adapted to Go.

var (
	// Pattern to detect RAR files
	rarPattern = regexp.MustCompile(`(?i)\.r(ar|\d+)$|\.part\d+\.rar$`)
	// Pattern to detect 7zip files
	sevenZipPattern = regexp.MustCompile(`(?i)\.7z$|\.7z\.\d+$`)
)

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

// HasPopularExtension reports whether file_path has a popular extension (case-insensitive)
// or matches known RAR or 7zip patterns (e.g., .rar, .r00, .partXX.rar, .7z, .7z.001).
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
	// Fallback to the package's RAR and 7zip detector on the basename
	base := filepath.Base(filePath)
	return rarPattern.MatchString(strings.ToLower(base)) || sevenZipPattern.MatchString(strings.ToLower(base))
}
