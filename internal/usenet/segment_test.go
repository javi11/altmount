package usenet

import (
	"testing"

	"github.com/acomagu/bufpipe"
)

func TestCloseSegments(t *testing.T) {
	r, w := bufpipe.New(nil)
	seg := &segment{
		reader: r,
		writer: w,
	}
	rg := &segmentRange{
		segments: []*segment{seg},
	}

	// Close it
	rg.CloseSegments()

	// Check if both are closed (by trying to write/read)
	// Note: bufpipe might return different errors but should unblock
	_, err := w.Write([]byte("test"))
	if err == nil {
		t.Error("expected error writing to closed pipe")
	}

	p := make([]byte, 4)
	_, err = r.Read(p)
	if err == nil {
		t.Error("expected error reading from closed pipe")
	}
}
