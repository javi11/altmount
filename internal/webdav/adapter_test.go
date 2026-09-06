package webdav

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kipsilabs/altmount/internal/config"
)

func newTestHandler(t *testing.T, loginRequired *bool) *Handler {
	t.Helper()

	getter := config.ConfigGetter(func() *config.Config {
		return &config.Config{
			Auth: config.AuthConfig{LoginRequired: loginRequired},
		}
	})

	h, err := NewHandler(
		&Config{User: "dav-user", Pass: "dav-pass", Prefix: "/webdav/"},
		nil,
		nil,
		nil,
		getter,
		nil,
	)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	return h
}

func boolPtr(v bool) *bool { return &v }

func TestHandlerAuthentication(t *testing.T) {
	tests := []struct {
		name          string
		loginRequired *bool
		user          string
		pass          string
		withBasicAuth bool
		wantStatus    int
	}{
		{
			name:          "login not required without credentials is allowed",
			loginRequired: boolPtr(false),
			wantStatus:    http.StatusOK,
		},
		{
			name:          "login not required with wrong credentials is rejected",
			loginRequired: boolPtr(false),
			user:          "dav-user",
			pass:          "wrong",
			withBasicAuth: true,
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "login not required with correct credentials is allowed",
			loginRequired: boolPtr(false),
			user:          "dav-user",
			pass:          "dav-pass",
			withBasicAuth: true,
			wantStatus:    http.StatusOK,
		},
		{
			name:          "login required without credentials is rejected",
			loginRequired: boolPtr(true),
			wantStatus:    http.StatusUnauthorized,
		},
		{
			name:          "login required with correct credentials is allowed",
			loginRequired: boolPtr(true),
			user:          "dav-user",
			pass:          "dav-pass",
			withBasicAuth: true,
			wantStatus:    http.StatusOK,
		},
		{
			name:       "unset login required defaults to required",
			wantStatus: http.StatusUnauthorized,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newTestHandler(t, tt.loginRequired)

			req := httptest.NewRequest(http.MethodOptions, "/webdav/", nil)
			if tt.withBasicAuth {
				req.SetBasicAuth(tt.user, tt.pass)
			}

			rec := httptest.NewRecorder()
			handler.GetHTTPHandler().ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}

			challenge := rec.Header().Get("WWW-Authenticate")
			if tt.wantStatus == http.StatusUnauthorized {
				if challenge != `Basic realm="BASIC WebDAV REALM"` {
					t.Errorf("WWW-Authenticate = %q, want the Basic challenge", challenge)
				}
			} else if challenge != "" {
				t.Errorf("WWW-Authenticate = %q, want empty", challenge)
			}
		})
	}
}

func TestHandlerAuthenticationNilConfigGetter(t *testing.T) {
	handler, err := NewHandler(
		&Config{User: "dav-user", Pass: "dav-pass", Prefix: "/webdav/"},
		nil,
		nil,
		nil,
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodOptions, "/webdav/", nil)
	rec := httptest.NewRecorder()
	handler.GetHTTPHandler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
