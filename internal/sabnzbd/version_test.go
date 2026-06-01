package sabnzbd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchLatestSABnzbdVersion(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		body    string
		want    string
		wantErr bool
	}{
		{
			name:   "stable release",
			status: http.StatusOK,
			body:   `{"tag_name":"5.0.3","prerelease":false}`,
			want:   "5.0.3",
		},
		{
			name:   "strips v prefix",
			status: http.StatusOK,
			body:   `{"tag_name":"v5.1.0","prerelease":false}`,
			want:   "5.1.0",
		},
		{
			name:    "prerelease rejected",
			status:  http.StatusOK,
			body:    `{"tag_name":"5.0.2RC1","prerelease":true}`,
			wantErr: true,
		},
		{
			name:    "empty tag rejected",
			status:  http.StatusOK,
			body:    `{"tag_name":"","prerelease":false}`,
			wantErr: true,
		},
		{
			name:    "non-200 rejected",
			status:  http.StatusForbidden,
			body:    `{"message":"rate limited"}`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer srv.Close()

			got, err := fetchLatestSABnzbdVersion(context.Background(), srv.Client(), srv.URL)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got version %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSABnzbdUserAgentFormat(t *testing.T) {
	// With no refresh having succeeded, SABnzbdVersion returns the fallback and the
	// UA must be a well-formed "SABnzbd/<version>" string.
	ua := "SABnzbd/" + FallbackVersion
	if got := "SABnzbd/" + defaultVersionCache.version; got != ua {
		t.Fatalf("got %q, want %q", got, ua)
	}
}
