package importer

import (
	"regexp"
)

var (
	// filename.part###.rar (e.g., movie.part001.rar, movie.part01.rar)
	partPattern = regexp.MustCompile(`^(.+)\.part(\d+)\.rar$`)
	// filename.### (numeric extensions like .001, .002)
	numericPattern = regexp.MustCompile(`^(.+)\.(\d+)$`)
	//filename.r## or filename.r### (e.g., movie.r00, movie.r01)
	rPattern             = regexp.MustCompile(`^(.+)\.r(\d+)$`)
	partPatternNumber    = regexp.MustCompile(`\.part(\d+)\.rar$`)
	rPatternNumber       = regexp.MustCompile(`\.r(\d+)$`)
	numericPatternNumber = regexp.MustCompile(`\.(\d+)$`)
)

func getPartNumber(originalFileName string) int {
	if matches := partPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		if num := parseInt(matches[1]); num >= 0 {
			return num
		}
	} else if matches := rPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		if num := parseInt(matches[1]); num >= 0 {
			return num + 1
		}
	} else if matches := numericPatternNumber.FindStringSubmatch(originalFileName); len(matches) > 1 {
		if num := parseInt(matches[1]); num >= 0 {
			return num
		}
	}

	return 0
}

// parseInt safely converts string to int
func parseInt(s string) int {
	num := 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			num = num*10 + int(r-'0')
		} else {
			return -1
		}
	}
	return num
}
