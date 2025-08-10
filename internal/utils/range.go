package utils

import (
	"errors"
	"strconv"
	"strings"
)

type RangeHeader struct {
	Start int64
	End   int64
}

// Decode interprets the RangeOption into an offset and a limit
//
// The offset is the start of the stream and the limit is how many
// bytes should be read from it.  If the limit is -1 then the stream
// should be read to the end.
func (o *RangeHeader) Decode(size int64) (offset, limit int64) {
	if o.Start >= 0 {
		offset = o.Start
		if o.End >= 0 {
			limit = o.End - o.Start + 1
		} else {
			limit = -1
		}
	} else {
		if o.End >= 0 {
			offset = size - o.End
		} else {
			offset = 0
		}
		limit = -1
	}
	return offset, limit
}

// ParseRangeHeader parses a RangeHeader from a Range: header.
// It only accepts single ranges.
func ParseRangeHeader(s string) (po *RangeHeader, err error) {
	const preamble = "bytes="
	if !strings.HasPrefix(s, preamble) {
		return nil, errors.New("range: header invalid: doesn't start with " + preamble)
	}
	s = s[len(preamble):]
	if strings.ContainsRune(s, ',') {
		return nil, errors.New("range: header invalid: contains multiple ranges which isn't supported")
	}
	dash := strings.IndexRune(s, '-')
	if dash < 0 {
		return nil, errors.New("range: header invalid: contains no '-'")
	}
	start, end := strings.TrimSpace(s[:dash]), strings.TrimSpace(s[dash+1:])
	o := RangeHeader{Start: -1, End: -1}
	if start != "" {
		o.Start, err = strconv.ParseInt(start, 10, 64)
		if err != nil || o.Start < 0 {
			return nil, errors.New("range: header invalid: bad start")
		}
	}
	if end != "" {
		o.End, err = strconv.ParseInt(end, 10, 64)
		if err != nil || o.End < 0 {
			return nil, errors.New("range: header invalid: bad end")
		}
	}

	return &o, nil
}

// FixRangeHeader looks through the slice of options and adjusts any
// RangeHeader~s found that request a fetch from the end into an
// absolute fetch using the size passed in and makes sure the range does
// not exceed filesize.
func FixRangeHeader(rh *RangeHeader, size int64) *RangeHeader {
	if size < 0 {
		// Can't do anything for unknown length objects
		return rh
	}

	fixed := rh
	if fixed.Start < 0 {
		fixed = &RangeHeader{Start: size - fixed.End, End: -1}
	}
	// If end is too big or undefined, fetch to the end
	if fixed.End > size || fixed.End < 0 {
		fixed = &RangeHeader{Start: fixed.Start, End: size - 1}
	}

	return fixed
}
