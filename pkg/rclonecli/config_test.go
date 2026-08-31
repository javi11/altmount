package rclonecli

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestCreateConfig_SendsObscureOpt pins the obscure flag on the config/create
// RC call.
//
// rclone decides for itself whether an incoming password is already obscured:
// it tries to reveal the value, and if that succeeds it assumes the caller
// handed it an obscured secret and stores it verbatim. That guess is wrong for
// any plaintext password that happens to be revealable, which includes ordinary
// base64-shaped generated passwords. The password is then stored un-obscured,
// reveals to a different string on every later read, and the internal mount
// authenticates against AltMount's own WebDAV with the wrong credential.
func TestCreateConfig_SendsObscureOpt(t *testing.T) {
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding config/create body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	m := &Manager{
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ctx:        context.Background(),
		rcPort:     u.Port(),
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}

	const plaintext = "s3cret-password"
	if err := m.createConfig(context.Background(), "altmount", "http://localhost:8080/webdav", "altmount", plaintext); err != nil {
		t.Fatalf("createConfig: %v", err)
	}

	opt, ok := body["opt"].(map[string]any)
	if !ok {
		t.Fatalf("config/create carried no opt block; rclone will guess at the password format. body=%v", body)
	}
	if obscure, _ := opt["obscure"].(bool); !obscure {
		t.Errorf("opt.obscure = %v, want true", opt["obscure"])
	}

	// The password must still go over as plaintext: obscure tells rclone to do
	// the obscuring itself, so pre-obscuring here would double-encode it.
	params, ok := body["parameters"].(map[string]any)
	if !ok {
		t.Fatalf("config/create carried no parameters block. body=%v", body)
	}
	if got := params["pass"]; got != plaintext {
		t.Errorf("parameters.pass = %v, want the plaintext %q", got, plaintext)
	}
}
