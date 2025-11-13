package archive

import "strconv"

// parseInt safely converts string to int
func ParseInt(s string) int {
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

// FormatInt converts an integer to a string
func FormatInt(n int) string {
	return strconv.Itoa(n)
}
