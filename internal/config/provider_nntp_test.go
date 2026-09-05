package config

import (
	"testing"
	"time"
)

func tlsProvider() ProviderConfig {
	return ProviderConfig{Host: "news.example", Port: 563, TLS: true, MaxConnections: 20}
}

func TestToNNTPProviderEnablesTLSSessionResumption(t *testing.T) {
	p := tlsProvider()
	np := p.ToNNTPProvider()
	if np.TLSConfig == nil || np.TLSConfig.ClientSessionCache == nil {
		t.Fatal("TLS providers must carry a session cache so reconnects resume instead of re-handshaking")
	}
}

func TestToNNTPProviderIdleTimeoutIsTwoMinutes(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().IdleTimeout; got != 2*time.Minute {
		t.Fatalf("IdleTimeout = %v, want 2m", got)
	}
}

func TestToNNTPProviderDefaultsMinConnectionsToTwo(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().MinConnections; got != 2 {
		t.Fatalf("MinConnections = %d, want 2 when unset", got)
	}
	p.MinConnectionsAlive = 5
	if got := p.ToNNTPProvider().MinConnections; got != 5 {
		t.Fatalf("explicit MinConnectionsAlive must win, got %d", got)
	}
	p.MinConnectionsAlive = 0
	p.MaxConnections = 1
	if got := p.ToNNTPProvider().MinConnections; got != 1 {
		t.Fatalf("default must be capped at MaxConnections, got %d", got)
	}
}

func TestToNNTPProviderSetsReconnectDelay(t *testing.T) {
	p := tlsProvider()
	if got := p.ToNNTPProvider().ReconnectDelay; got != 30*time.Second {
		t.Fatalf("ReconnectDelay = %v, want 30s so a provider removed on 502 comes back", got)
	}
}
