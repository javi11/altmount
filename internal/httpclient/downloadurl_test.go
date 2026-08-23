package httpclient

import "testing"

// TestValidateDownloadURL pins which indexer-supplied download targets are
// allowed. Indexer search responses drive these fetches, so a hostile indexer
// must not be able to aim AltMount at loopback, private or link-local
// addresses — cloud instance-metadata endpoints in particular.
func TestValidateDownloadURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		wantErr bool
	}{
		{"public https host", "https://indexer.example.com/getnzb/abc?apikey=k", false},
		{"public http host", "http://indexer.example.com/getnzb/abc", false},
		{"public literal IP", "https://93.184.216.34/getnzb/abc", false},

		{"empty", "", true},
		{"no scheme", "indexer.example.com/getnzb", true},
		{"file scheme", "file:///etc/passwd", true},
		{"gopher scheme", "gopher://example.com/", true},
		{"no host", "http:///getnzb/abc", true},

		{"loopback IPv4", "http://127.0.0.1:8080/getnzb", true},
		{"loopback IPv6", "http://[::1]/getnzb", true},
		{"localhost", "http://localhost:9696/getnzb", true},
		{"localhost subdomain", "http://indexer.localhost/getnzb", true},
		{"unspecified address", "http://0.0.0.0/getnzb", true},
		{"private 10.x", "http://10.0.0.5/getnzb", true},
		{"private 192.168.x", "http://192.168.1.10/getnzb", true},
		{"private 172.16.x", "http://172.16.4.4/getnzb", true},
		{"link-local metadata endpoint", "http://169.254.169.254/latest/meta-data/", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDownloadURL(tt.raw)
			if tt.wantErr && err == nil {
				t.Fatalf("ValidateDownloadURL(%q) = nil, want error", tt.raw)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("ValidateDownloadURL(%q) = %v, want nil", tt.raw, err)
			}
		})
	}
}
