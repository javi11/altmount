package config

import "testing"

func TestDefaultStreamHeadroom_ScalesWithPoolSize(t *testing.T) {
	// The cost of a reservation is what fraction of the pool it takes, so the
	// default has to scale with connection count. An absolute default costs a
	// 10-connection pool 80% of its import capacity and a 100-connection pool 8%.
	tests := []struct {
		conns int
		want  int
	}{
		{conns: 0, want: minStreamHeadroom},
		{conns: 1, want: minStreamHeadroom},
		{conns: 8, want: minStreamHeadroom},
		{conns: 10, want: 2},
		{conns: 20, want: 5},
		{conns: 50, want: 12},
		{conns: 100, want: 25},
		{conns: 200, want: 50},
	}
	for _, tt := range tests {
		if got := DefaultStreamHeadroom(tt.conns); got != tt.want {
			t.Errorf("DefaultStreamHeadroom(%d) = %d, want %d", tt.conns, got, tt.want)
		}
	}
}

func TestDefaultStreamHeadroom_NeverStarvesImport(t *testing.T) {
	// Whatever the pool size, one stream must never take so much that import is
	// left with a token trickle. The proportional rule keeps the loss constant;
	// this pins that it stays under a third for every plausible pool.
	for conns := 1; conns <= 500; conns++ {
		h := DefaultStreamHeadroom(conns)
		if conns >= 8 && h*3 > conns {
			t.Fatalf("conns=%d: default headroom %d takes more than a third of the pool", conns, h)
		}
		if h < minStreamHeadroom {
			t.Fatalf("conns=%d: headroom %d below the floor %d", conns, h, minStreamHeadroom)
		}
	}
}

func TestGetStreamHeadroomConnections_ExplicitOverridesDerived(t *testing.T) {
	enabled := true
	base := func() *Config {
		return &Config{Providers: []ProviderConfig{
			{MaxConnections: 100, Enabled: &enabled},
		}}
	}

	c := base()
	if got, want := c.GetStreamHeadroomConnections(), DefaultStreamHeadroom(100); got != want {
		t.Errorf("unset: got %d, want derived %d", got, want)
	}

	n := 32
	c = base()
	c.Import.StreamHeadroomConnections = &n
	if got := c.GetStreamHeadroomConnections(); got != 32 {
		t.Errorf("explicit 32: got %d, want 32", got)
	}

	zero := 0
	c = base()
	c.Import.StreamHeadroomConnections = &zero
	if got := c.GetStreamHeadroomConnections(); got != 0 {
		t.Errorf("explicit 0 must disable the reservation, got %d", got)
	}
}

func TestGetStreamHeadroomConnections_TracksConnectionCount(t *testing.T) {
	// The derived default must follow provider capacity, so adding connections
	// widens the reservation rather than leaving it pinned to the old pool size.
	enabled := true
	small := &Config{Providers: []ProviderConfig{{MaxConnections: 20, Enabled: &enabled}}}
	large := &Config{Providers: []ProviderConfig{{MaxConnections: 200, Enabled: &enabled}}}

	if small.GetStreamHeadroomConnections() >= large.GetStreamHeadroomConnections() {
		t.Errorf("headroom did not grow with the pool: 20 conns -> %d, 200 conns -> %d",
			small.GetStreamHeadroomConnections(), large.GetStreamHeadroomConnections())
	}
}
