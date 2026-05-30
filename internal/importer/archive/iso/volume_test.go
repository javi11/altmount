package iso

import (
	"bytes"
	"io"
	"testing"
)

// buildPVD constructs a 17-sector buffer with a synthetic Primary Volume
// Descriptor placed at sector 16. The remaining bytes are zero-filled.
func buildPVD(label string, typeCode byte, identifier string) io.ReadSeeker {
	buf := make([]byte, 17*iso9660SectorSize)
	pvd := buf[16*iso9660SectorSize:]
	pvd[0] = typeCode
	copy(pvd[1:6], identifier)
	// Volume identifier field is 32 bytes, space-padded.
	field := make([]byte, 32)
	for i := range field {
		field[i] = ' '
	}
	copy(field, label)
	copy(pvd[40:72], field)
	return bytes.NewReader(buf)
}

func TestReadVolumeLabel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		rs   io.ReadSeeker
		want string
	}{
		{
			name: "Avatar disc 1 label",
			rs:   buildPVD("AVATAR_FIRE_AND_ASH_DISC_1", 1, "CD001"),
			want: "AVATAR_FIRE_AND_ASH_DISC_1",
		},
		{
			name: "padded short label trimmed",
			rs:   buildPVD("FOO", 1, "CD001"),
			want: "FOO",
		},
		{
			name: "wrong type code",
			rs:   buildPVD("ANYTHING", 2, "CD001"),
			want: "",
		},
		{
			name: "wrong identifier",
			rs:   buildPVD("ANYTHING", 1, "BAD!?"),
			want: "",
		},
		{
			name: "short input (no sector 16)",
			rs:   bytes.NewReader(make([]byte, 1024)),
			want: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := ReadVolumeLabel(tc.rs)
			if got != tc.want {
				t.Errorf("ReadVolumeLabel = %q, want %q", got, tc.want)
			}
		})
	}
}
