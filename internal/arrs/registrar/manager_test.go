package registrar

import "testing"

func TestIsAltmountDownloadClient(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{AltmountDownloadClientName, true}, // exact registered name
		{"Altmount", true},                // common manual name
		{"altmount", true},                // lowercase
		{"AltMount (SABnzbd)", true},
		{"My AltMount SAB", true},
		{"", false},
		{"qBittorrent", false},
		{"SABnzbd", false},
		{"NZBGet", false},
	}
	for _, tt := range tests {
		if got := IsAltmountDownloadClient(tt.name); got != tt.want {
			t.Errorf("IsAltmountDownloadClient(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}
