package parser

import (
	"regexp"
	"strconv"

	"github.com/javi11/nzbparser"
)

// subjectSizePatterns pull the byte count posting tools write at the end of a
// subject line: `[1/5] - "movie.mkv" yEnc (1/170) 710427259`. It is the file's
// exact decoded length, which the NZB's own segment sizes cannot give.
var subjectSizePatterns = []*regexp.Regexp{
	regexp.MustCompile(`\(\d+/\d+\)\s+(\d+)\s*$`),
	regexp.MustCompile(`(?i)\byEnc\s+(\d+)\s*$`),
}

// sizeFromSubject reads the poster's declared file size out of a subject line,
// returning zero when there is none or when it does not stand up against the
// encoded total. A decoded file never outweighs the articles carrying it and
// yEnc never doubles what it encodes, so a trailing number outside that band
// is a date, a part count or a password, not a size.
func sizeFromSubject(subject string, encoded int64) int64 {
	if encoded <= 0 {
		return 0
	}
	for _, pattern := range subjectSizePatterns {
		match := pattern.FindStringSubmatch(subject)
		if len(match) < 2 {
			continue
		}
		size, err := strconv.ParseInt(match[1], 10, 64)
		if err != nil || size <= 0 || size > encoded || size < encoded/2 {
			continue
		}
		return size
	}
	return 0
}

// declaredFileSize is sizeFromSubject over an NZB file's own segments.
func declaredFileSize(file *nzbparser.NzbFile) int64 {
	if file == nil {
		return 0
	}
	var encoded int64
	for _, seg := range file.Segments {
		encoded += int64(seg.Bytes)
	}
	return sizeFromSubject(file.Subject, encoded)
}
