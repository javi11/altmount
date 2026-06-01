package clients

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestSportarr points a Sportarr client at a stub server returning body.
func newTestSportarr(t *testing.T, body string) (*Sportarr, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	return NewSportarr(srv.URL, "key", srv.Client()), srv.Close
}

// Current Sportarr (/api/queue): status is the numeric DownloadStatus enum,
// statusMessages is a bare string array, downloadClient is an object. The old
// struct decoded these as string / []object / string and failed the whole
// unmarshal, leaving imports attributed to "Unknown".
func TestGetQueue_CurrentSportarrShape(t *testing.T) {
	body := `[
	  {
	    "id": 1,
	    "title": "Some Event 2026",
	    "downloadId": "abc-123",
	    "indexer": "MyIndexer",
	    "status": 3,
	    "protocol": "usenet",
	    "downloadClient": {"id": 2, "name": "AltMount", "postImportCategory": "sports"},
	    "statusMessages": ["Automatic import is not possible", "Check your settings"]
	  }
	]`
	client, closeFn := newTestSportarr(t, body)
	defer closeFn()

	items, err := client.GetQueue(context.Background())
	if err != nil {
		t.Fatalf("GetQueue failed to parse current Sportarr shape: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	q := items[0]
	if q.DownloadID != "abc-123" {
		t.Errorf("DownloadID = %q, want abc-123", q.DownloadID)
	}
	if q.Indexer != "MyIndexer" {
		t.Errorf("Indexer = %q, want MyIndexer", q.Indexer)
	}
	if q.Status != "completed" {
		t.Errorf("Status = %q, want completed (enum 3)", q.Status)
	}
	if q.DownloadClient.Name != "AltMount" {
		t.Errorf("DownloadClient.Name = %q, want AltMount", q.DownloadClient.Name)
	}
	if len(q.StatusMessages) != 2 || q.StatusMessages[0].Messages[0] != "Automatic import is not possible" {
		t.Errorf("StatusMessages not normalized from string array: %+v", q.StatusMessages)
	}
}

// Legacy/Servarr shape: status string, statusMessages objects, downloadClient
// string. The tolerant types must still accept this.
func TestGetQueue_LegacyServarrShape(t *testing.T) {
	body := `{"records": [
	  {
	    "id": 9,
	    "downloadId": "legacy-9",
	    "indexer": "OldIndexer",
	    "status": "completed",
	    "downloadClient": "AltMount",
	    "statusMessages": [{"title": "t", "messages": ["m1", "m2"]}]
	  }
	]}`
	client, closeFn := newTestSportarr(t, body)
	defer closeFn()

	items, err := client.GetQueue(context.Background())
	if err != nil {
		t.Fatalf("GetQueue failed on legacy shape: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	q := items[0]
	if q.DownloadID != "legacy-9" || q.Indexer != "OldIndexer" {
		t.Errorf("unexpected item: %+v", q)
	}
	if q.Status != "completed" {
		t.Errorf("Status = %q, want completed", q.Status)
	}
	if q.DownloadClient.Name != "AltMount" {
		t.Errorf("DownloadClient.Name = %q, want AltMount", q.DownloadClient.Name)
	}
	if len(q.StatusMessages) != 1 || q.StatusMessages[0].Title != "t" {
		t.Errorf("StatusMessages object shape not preserved: %+v", q.StatusMessages)
	}
}
