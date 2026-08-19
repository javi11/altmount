package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
	_ "github.com/mattn/go-sqlite3"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/par2repair"
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
	app.Get("/api/par2repair", s.handleListPar2Repair)
	app.Delete("/api/par2repair", s.handleCancelAllPar2Repair)
	app.Delete("/api/par2repair/:id", s.handleCancelPar2Repair)
	return app
}

// newPar2RepairAPIRepo builds a real repository over an in-memory sqlite with
// the migration-035 schema (mirrors internal/database/par2repair_repository_test.go).
func newPar2RepairAPIRepo(t *testing.T) *database.Par2RepairRepository {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	_, err = db.Exec(`
		CREATE TABLE par2_repair_jobs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			file_path TEXT NOT NULL,
			nzb_path TEXT,
			status TEXT NOT NULL DEFAULT 'pending',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT,
			failing_segment_id TEXT,
			dead_segment_ids TEXT,
			next_attempt_at TIMESTAMP,
			started_at TIMESTAMP,
			finished_at TIMESTAMP,
			created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE UNIQUE INDEX idx_par2_repair_active ON par2_repair_jobs(file_path)
			WHERE status IN ('pending','running');
	`)
	if err != nil {
		t.Fatal(err)
	}
	return database.NewPar2RepairRepository(db, database.DialectSQLite)
}

func TestHandleListPar2Repair(t *testing.T) {
	repo := newPar2RepairAPIRepo(t)
	ctx := context.Background()
	if _, err := repo.Enqueue(ctx, "/movies/a.mkv", "<seg@x>"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Enqueue(ctx, "/movies/b.mkv", ""); err != nil {
		t.Fatal(err)
	}
	job, err := repo.ClaimNext(ctx, time.Now().UTC())
	if err != nil || job == nil {
		t.Fatalf("claim failed: %v", err)
	}

	s := &Server{par2RepairRepo: repo}
	app := par2TestApp(s)
	resp, err := app.Test(httptest.NewRequest("GET", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	raw, _ := io.ReadAll(resp.Body)
	var envelope struct {
		Success bool                    `json:"success"`
		Data    []Par2RepairJobResponse `json:"data"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatalf("unmarshal %s: %v", raw, err)
	}
	if !envelope.Success || len(envelope.Data) != 2 {
		t.Fatalf("envelope = %s", raw)
	}
	byPath := map[string]Par2RepairJobResponse{}
	for _, j := range envelope.Data {
		byPath[j.FilePath] = j
	}
	a := byPath["/movies/a.mkv"]
	if a.Status != "running" || a.StartedAt == nil {
		t.Fatalf("a.mkv row = %+v", a)
	}
	b := byPath["/movies/b.mkv"]
	if b.Status != "pending" || b.LastError != nil || b.ID == 0 || b.CreatedAt.IsZero() {
		t.Fatalf("b.mkv row = %+v", b)
	}
}

func TestHandleListPar2RepairUnavailable(t *testing.T) {
	app := par2TestApp(&Server{})
	resp, err := app.Test(httptest.NewRequest("GET", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503 when repo unset", resp.StatusCode)
	}
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

// fakeCanceller is an enqueuer that also cancels, recording the IDs it saw.
type fakeCanceller struct {
	fakeEnqueuer
	cancelled []int64
	err       error
}

func (f *fakeCanceller) Cancel(_ context.Context, jobID int64) error {
	f.cancelled = append(f.cancelled, jobID)
	return f.err
}

func TestHandleCancelPar2Repair(t *testing.T) {
	canceller := &fakeCanceller{}
	app := par2TestApp(&Server{par2Repair: canceller})

	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair/7", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(canceller.cancelled) != 1 || canceller.cancelled[0] != 7 {
		t.Fatalf("cancelled = %v, want [7]", canceller.cancelled)
	}
}

func TestHandleCancelPar2RepairErrors(t *testing.T) {
	tests := []struct {
		name       string
		server     *Server
		path       string
		wantStatus int
	}{
		{"bad id", &Server{par2Repair: &fakeCanceller{}}, "/api/par2repair/abc", 400},
		{"zero id", &Server{par2Repair: &fakeCanceller{}}, "/api/par2repair/0", 400},
		{"no service", &Server{}, "/api/par2repair/7", 503},
		{"enqueue-only service", &Server{par2Repair: &fakeEnqueuer{}}, "/api/par2repair/7", 503},
		{
			"unknown job",
			&Server{par2Repair: &fakeCanceller{err: par2repair.ErrJobNotFound}},
			"/api/par2repair/7",
			404,
		},
		{
			"will not stop",
			&Server{par2Repair: &fakeCanceller{err: errors.New("did not stop within 30s")}},
			"/api/par2repair/7",
			500,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			app := par2TestApp(tt.server)
			resp, err := app.Test(httptest.NewRequest("DELETE", tt.path, nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != tt.wantStatus {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.wantStatus)
			}
		})
	}
}

func TestHandleCancelAllPar2Repair(t *testing.T) {
	repo := newPar2RepairAPIRepo(t)
	ctx := context.Background()
	for _, p := range []string{"/movies/a.mkv", "/movies/b.mkv"} {
		if _, err := repo.Enqueue(ctx, p, ""); err != nil {
			t.Fatal(err)
		}
	}
	// Cancel deletes rows in the real service; the fake does not, so stop
	// after the first pass by reporting every job as already gone.
	canceller := &fakeCanceller{err: par2repair.ErrJobNotFound}
	app := par2TestApp(&Server{par2Repair: canceller, par2RepairRepo: repo})

	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(canceller.cancelled) != 2 {
		t.Fatalf("cancelled = %v, want both jobs attempted once", canceller.cancelled)
	}

	body, _ := io.ReadAll(resp.Body)
	var payload struct {
		Data struct {
			Cancelled int `json:"cancelled"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("unmarshal %s: %v", body, err)
	}
	// Both were already gone, so nothing counts as cancelled.
	if payload.Data.Cancelled != 0 {
		t.Fatalf("cancelled count = %d, want 0", payload.Data.Cancelled)
	}
}

func TestHandleCancelAllPar2RepairUnavailable(t *testing.T) {
	app := par2TestApp(&Server{par2Repair: &fakeCanceller{}})
	resp, err := app.Test(httptest.NewRequest("DELETE", "/api/par2repair", nil))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d, want 503 when the repo is unset", resp.StatusCode)
	}
}
