package api

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/javi11/altmount/internal/config"
)

// TestStremioNzbTTLHoursZeroRoundTrip verifies that an explicit NzbTTLHours of 0
// ("cache forever / disable expiry") survives serialization to the API response.
// A dropped 0 causes the frontend to fall back to its default of 24, silently
// overwriting the user's choice on the next save.
func TestStremioNzbTTLHoursZeroRoundTrip(t *testing.T) {
	enabled := true
	cfg := &config.Config{
		Stremio: config.StremioConfig{
			Enabled:     &enabled,
			NzbTTLHours: 0,
		},
	}

	resp := ToConfigAPIResponse(cfg, "")

	data, err := json.Marshal(resp.Stremio)
	if err != nil {
		t.Fatalf("marshal stremio response: %v", err)
	}

	if !strings.Contains(string(data), `"nzb_ttl_hours":0`) {
		t.Errorf("expected nzb_ttl_hours:0 to be present in response JSON, got: %s", data)
	}
}
