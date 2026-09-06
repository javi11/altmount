package rar

import (
	"errors"

	"github.com/javi11/rardecode/v2"
)

// IsCorruptionError reports whether an archive analysis failure was caused by
// corrupt article DATA — bytes that downloaded fine but no longer decode as
// valid RAR structure. The fast-fail sweep only sees MISSING articles, so
// this class of damage sails through it and surfaces here; it is exactly what
// PAR2 recovery data can rebuild, so callers use it to defer the import for a
// repair instead of failing it.
//
// Errors reach this through the pipeline's wrapping (NonRetryableError around
// the rardecode sentinel, joined per archive group), all errors.Is-traversable.
func IsCorruptionError(err error) bool {
	if err == nil {
		return false
	}
	for _, sentinel := range []error{
		rardecode.ErrBadHeaderCRC,
		rardecode.ErrCorruptFileHeader,
		rardecode.ErrCorruptBlockHeader,
		rardecode.ErrCorruptEncryptData,
	} {
		if errors.Is(err, sentinel) {
			return true
		}
	}
	return false
}
