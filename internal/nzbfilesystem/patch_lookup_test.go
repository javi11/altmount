package nzbfilesystem

import (
	"bytes"
	"testing"
)

type fakePatchSource struct {
	patches map[string][]byte
}

func (f *fakePatchSource) Get(messageID string) ([]byte, bool) {
	p, ok := f.patches[messageID]
	return p, ok
}

// The reader hands segment IDs as stored in metadata (possibly bracketed);
// the patch store keys on the bare form. patchLookup must normalize.
func TestPatchLookupNormalizesMessageID(t *testing.T) {
	payload := []byte{1, 2, 3}
	mvf := &MetadataVirtualFile{
		patchSource: &fakePatchSource{patches: map[string][]byte{"dead@test": payload}},
	}

	if got := mvf.patchLookup("<dead@test>"); !bytes.Equal(got, payload) {
		t.Fatalf("bracketed lookup = %v, want payload", got)
	}
	if got := mvf.patchLookup("dead@test"); !bytes.Equal(got, payload) {
		t.Fatalf("bare lookup = %v, want payload", got)
	}
	if got := mvf.patchLookup("<other@test>"); got != nil {
		t.Fatalf("miss = %v, want nil", got)
	}
}
