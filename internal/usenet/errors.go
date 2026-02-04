package usenet

import "errors"

var (
	ErrCorruptedNzb = errors.New("corrupted nzb")
	ErrLimitReached = errors.New("limit reached")
)
