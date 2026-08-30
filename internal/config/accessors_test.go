package config

import (
	"testing"
	"time"
)

func TestGetImportVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetImportVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Import.VerifyContent = &enabled
	if !c.GetImportVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetImportVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
	secs := 5
	c.Import.VerifyContentTimeoutSeconds = &secs
	if got := c.GetImportVerifyContentTimeout(); got != 5*time.Second {
		t.Errorf("got %v, want 5s", got)
	}
}

func TestGetHealthVerifyContent(t *testing.T) {
	c := &Config{}
	if c.GetHealthVerifyContent() {
		t.Error("expected false by default")
	}
	enabled := true
	c.Health.VerifyContent = &enabled
	if !c.GetHealthVerifyContent() {
		t.Error("expected true when set")
	}
}

func TestGetHealthVerifyContentTimeout(t *testing.T) {
	c := &Config{}
	if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("got %v, want 15s default", got)
	}
}

func TestDefaultConfig_VerifyContentDisabledByDefault(t *testing.T) {
	c := DefaultConfig()
	if c.GetImportVerifyContent() {
		t.Error("expected import verify_content to default to false")
	}
	if c.GetHealthVerifyContent() {
		t.Error("expected health verify_content to default to false")
	}
}

func TestDefaultConfig_VerifyContentTimeoutDefaultsTo15Seconds(t *testing.T) {
	c := DefaultConfig()
	if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("import: got %v, want 15s default", got)
	}
	if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
		t.Errorf("health: got %v, want 15s default", got)
	}
}

func TestGetImportVerifyContentTimeout_NonPositiveFallsBackTo15Seconds(t *testing.T) {
	for _, secs := range []int{0, -1, -30} {
		c := &Config{}
		c.Import.VerifyContentTimeoutSeconds = &secs
		if got := c.GetImportVerifyContentTimeout(); got != 15*time.Second {
			t.Errorf("secs=%d: got %v, want 15s fallback", secs, got)
		}
	}
}

func TestGetHealthVerifyContentTimeout_NonPositiveFallsBackTo15Seconds(t *testing.T) {
	for _, secs := range []int{0, -1, -30} {
		c := &Config{}
		c.Health.VerifyContentTimeoutSeconds = &secs
		if got := c.GetHealthVerifyContentTimeout(); got != 15*time.Second {
			t.Errorf("secs=%d: got %v, want 15s fallback", secs, got)
		}
	}
}

func TestValidate_ImportVerifyContentTimeoutSeconds(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Health.Enabled = nil
		return c
	}

	zero := 0
	negative := -5
	positive := 10

	tests := []struct {
		name    string
		secs    *int
		wantErr bool
	}{
		{"nil is valid (falls back to default)", nil, false},
		{"positive is valid", &positive, false},
		{"zero is invalid", &zero, true},
		{"negative is invalid", &negative, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			c.Import.VerifyContentTimeoutSeconds = tt.secs
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no validation error, got %v", err)
			}
		})
	}
}

func TestValidate_HealthVerifyContentTimeoutSeconds(t *testing.T) {
	base := func() *Config {
		c := DefaultConfig()
		c.Health.Enabled = nil
		return c
	}

	zero := 0
	negative := -5
	positive := 10

	tests := []struct {
		name    string
		secs    *int
		wantErr bool
	}{
		{"nil is valid (falls back to default)", nil, false},
		{"positive is valid", &positive, false},
		{"zero is invalid", &zero, true},
		{"negative is invalid", &negative, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base()
			c.Health.VerifyContentTimeoutSeconds = tt.secs
			err := c.Validate()
			if tt.wantErr && err == nil {
				t.Error("expected validation error, got nil")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("expected no validation error, got %v", err)
			}
		})
	}
}

func TestStatConcurrency(t *testing.T) {
	enabled := true
	disabled := false

	tests := []struct {
		name string
		cfg  *Config
		want int
	}{
		{
			name: "no providers falls back to the stat_inflight_requests default",
			cfg:  &Config{},
			want: 100,
		},
		{
			name: "unset depth falls back to the same default applied at normalization",
			cfg:  &Config{Providers: []ProviderConfig{{MaxConnections: 10, Enabled: &enabled}}},
			want: 100,
		},
		{
			name: "connection count does not bound STAT concurrency",
			cfg: &Config{Providers: []ProviderConfig{
				{MaxConnections: 2, StatInflightRequests: 100, Enabled: &enabled},
			}},
			want: 100,
		},
		{
			name: "follows the configured depth below the default",
			cfg: &Config{Providers: []ProviderConfig{
				{MaxConnections: 50, StatInflightRequests: 20, Enabled: &enabled},
			}},
			want: 20,
		},
		{
			name: "takes the deepest enabled provider",
			cfg: &Config{Providers: []ProviderConfig{
				{MaxConnections: 10, StatInflightRequests: 20, Enabled: &enabled},
				{MaxConnections: 10, StatInflightRequests: 200, Enabled: &enabled},
			}},
			want: 200,
		},
		{
			name: "ignores disabled providers",
			cfg: &Config{Providers: []ProviderConfig{
				{MaxConnections: 10, StatInflightRequests: 20, Enabled: &enabled},
				{MaxConnections: 10, StatInflightRequests: 400, Enabled: &disabled},
			}},
			want: 20,
		},
		{
			name: "caps so a chunked sweep keeps its early-termination boundaries",
			cfg: &Config{Providers: []ProviderConfig{
				{MaxConnections: 10, StatInflightRequests: 5000, Enabled: &enabled},
			}},
			want: maxStatConcurrency,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.StatConcurrency(); got != tt.want {
				t.Fatalf("StatConcurrency() = %d, want %d", got, tt.want)
			}
		})
	}
}

// A connection count and a STAT pipeline depth are different quantities:
// stat_inflight_requests is how many checks nntppool pipelines down ONE
// connection, so deriving a connection budget from it reports a number that
// has nothing to do with how many connections exist.
func TestGetMaxConnectionsForHealthChecks_FollowsProviderConnectionsWhenUnset(t *testing.T) {
	enabled := true
	cfg := &Config{Providers: []ProviderConfig{
		{MaxConnections: 10, StatInflightRequests: 250, Enabled: &enabled},
	}}

	if got := cfg.GetMaxConnectionsForHealthChecks(); got != 10 {
		t.Fatalf("GetMaxConnectionsForHealthChecks() = %d, want 10 (the providers' connection count)", got)
	}

	cfg.Health.MaxConnectionsForHealthChecks = 30
	if got := cfg.GetMaxConnectionsForHealthChecks(); got != 30 {
		t.Fatalf("GetMaxConnectionsForHealthChecks() = %d, want 30 (explicit setting wins)", got)
	}
}

// TotalProviderConnections returns 0 when nothing is configured. Passing that
// through would collapse a health sweep to a chunk size of 1.
func TestGetMaxConnectionsForHealthChecks_FloorsWhenNoProviders(t *testing.T) {
	cfg := &Config{}

	if got := cfg.GetMaxConnectionsForHealthChecks(); got != defaultHealthCheckConnections {
		t.Fatalf("GetMaxConnectionsForHealthChecks() = %d, want %d", got, defaultHealthCheckConnections)
	}
}
