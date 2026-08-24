package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/config"
)

func TestImportPinSymlinkTimestampRoundTrip(t *testing.T) {
	ts := "2026-08-21T14:30:00Z"
	cfg := &config.Config{
		Import: config.ImportConfig{
			PinSymlinkTimestamp: &ts,
		},
	}

	resp := ToConfigAPIResponse(cfg, "")

	if resp.Import.PinSymlinkTimestamp == nil {
		t.Fatalf("expected PinSymlinkTimestamp in response, got nil")
	}

	if *resp.Import.PinSymlinkTimestamp != ts {
		t.Errorf("PinSymlinkTimestamp = %q; want %q", *resp.Import.PinSymlinkTimestamp, ts)
	}

	data, err := json.Marshal(resp.Import)
	if err != nil {
		t.Fatalf("marshal import response: %v", err)
	}

	if !strings.Contains(string(data), `"pin_symlink_timestamp":"2026-08-21T14:30:00Z"`) {
		t.Errorf("expected pin_symlink_timestamp in response JSON, got: %s", data)
	}
}
