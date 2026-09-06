package rar

import (
	"errors"
	"fmt"
	"testing"

	"github.com/javi11/rardecode/v2"

	alterrors "github.com/javi11/altmount/internal/errors"
)

// IsCorruptionError must recognize rardecode's corruption sentinels through
// the exact wrapping the import pipeline applies: the RAR processor wraps the
// iterate error in a NonRetryableError, and the aggregator joins per-group
// errors — the same shape "rardecode: bad header crc" arrived in when a
// corrupt-but-present article broke a release's analysis.
func TestIsCorruptionError(t *testing.T) {
	wrapLikePipeline := func(err error) error {
		return errors.Join(alterrors.NewNonRetryableError(
			fmt.Sprintf("failed to iterate RAR archive %q", "a.part01.rar"), err))
	}

	corruptionSentinels := []error{
		rardecode.ErrBadHeaderCRC,
		rardecode.ErrCorruptFileHeader,
		rardecode.ErrCorruptBlockHeader,
	}
	for _, sentinel := range corruptionSentinels {
		if !IsCorruptionError(wrapLikePipeline(sentinel)) {
			t.Errorf("IsCorruptionError(%v) = false, want true", sentinel)
		}
	}

	notCorruption := []error{
		nil,
		errors.New("dial tcp: connection refused"),
		alterrors.NewNonRetryableError("no valid files found in RAR archive", nil),
	}
	for _, err := range notCorruption {
		if IsCorruptionError(err) {
			t.Errorf("IsCorruptionError(%v) = true, want false", err)
		}
	}
}
