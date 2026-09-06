package holes

import (
	"strconv"
	"strings"
)

// placeholderPrefix marks a segment that stands in for an article the NZB
// itself never listed (a gap in its segment numbering). Such a segment has no
// article to fetch: it is a hole known before any provider is asked, sized so
// the bytes after it keep their true offsets.
const placeholderPrefix = "altmount-gap-"

// PlaceholderID builds the message id of a synthetic gap segment. number is
// the missing segment's 1-based position; salt ties the id to its file (any
// id of that file works) so two files' gaps never collide in a store index.
func PlaceholderID(number int, salt string) string {
	return placeholderPrefix + strconv.Itoa(number) + "-" + salt
}

// IsPlaceholderID reports whether id names a synthetic gap segment rather
// than a real article. Angle brackets are tolerated for callers that carry
// wire-form ids.
func IsPlaceholderID(id string) bool {
	return strings.HasPrefix(strings.TrimPrefix(id, "<"), placeholderPrefix)
}
