package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

type fakeEnqueuer struct {
	calls []struct{ path, segID string }
}

func (f *fakeEnqueuer) Enqueue(_ context.Context, filePath, failingSegmentID string) {
	f.calls = append(f.calls, struct{ path, segID string }{filePath, failingSegmentID})
}

func par2TestApp(s *Server) *fiber.App {
	app := fiber.New()
	app.Post("/api/par2repair", s.handlePar2Repair)
	return app
}

func TestHandlePar2RepairQueues(t *testing.T) {
	enq := &fakeEnqueuer{}
	s := &Server{par2Repair: enq}
	app := par2TestApp(s)

	body, _ := json.Marshal(Par2RepairRequest{FilePath: "/movies/a.mkv"})
	req := httptest.NewRequest("POST", "/api/par2repair", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(enq.calls) != 1 || enq.calls[0].path != "/movies/a.mkv" {
		t.Fatalf("enqueue calls = %+v", enq.calls)
	}
}

func TestHandlePar2RepairValidation(t *testing.T) {
	tests := []struct {
		name       string
		server     *Server
		body       string
		wantStatus int
	}{
		{"missing file_path", &Server{par2Repair: &fakeEnqueuer{}}, `{}`, 400},
		{"invalid json", &Server{par2Repair: &fakeEnqueuer{}}, `{`, 400},
		{"service unavailable", &Server{}, `{"file_path":"/a.mkv"}`, 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := par2TestApp(tt.server)
			req := httptest.NewRequest("POST", "/api/par2repair", bytes.NewReader([]byte(tt.body)))
			req.Header.Set("Content-Type", "application/json")
			resp, err := app.Test(req)
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}
