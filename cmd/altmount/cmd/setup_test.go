package cmd

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// recordingWriter captures SetWriteDeadline calls so tests can observe
// whether the streaming-route deadline exemption was applied.
type recordingWriter struct {
	http.ResponseWriter
	deadlines []time.Time
}

func (w *recordingWriter) SetWriteDeadline(t time.Time) error {
	w.deadlines = append(w.deadlines, t)
	return nil
}

func TestIsStreamingRoute(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/api/files/stream?path=movies/dune.mkv&download_key=k", true},
		{"/api/files/stream", true},
		{"/webdav/complete/movies/Dune.mkv", true},
		{"/webdav", true},
		{"/api/logs/stream", true},
		{"/api/queue/stream", true},
		{"/api/health/stream", true},
		{"/api/system/config", false},
		{"/api/import/config", false},
		{"/", false},
		{"/api/files/streamers", true}, // prefix match mirrors router behavior
	}

	for _, tt := range tests {
		if got := isStreamingRoute(tt.path); got != tt.want {
			t.Errorf("isStreamingRoute(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestClearWriteDeadlineRemovesDeadline(t *testing.T) {
	rec := &recordingWriter{ResponseWriter: httptest.NewRecorder()}
	clearWriteDeadline(rec)

	if len(rec.deadlines) != 1 {
		t.Fatalf("SetWriteDeadline called %d times, want 1", len(rec.deadlines))
	}
	if !rec.deadlines[0].IsZero() {
		t.Fatalf("SetWriteDeadline = %v, want zero time (deadline removed)", rec.deadlines[0])
	}
}
