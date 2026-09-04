package config

import "testing"

func TestProviderFingerprintIsOrderIndependentAndSkipsDisabled(t *testing.T) {
	on, off := true, false
	a := &Config{Providers: []ProviderConfig{{Host: "a", Port: 563, Enabled: &on}, {Host: "b", Port: 119, Enabled: &on}}}
	b := &Config{Providers: []ProviderConfig{{Host: "b", Port: 119, Enabled: &on}, {Host: "a", Port: 563, Enabled: &on}}}
	if a.ProviderFingerprint() != b.ProviderFingerprint() {
		t.Fatal("fingerprint must not depend on provider order")
	}
	c := &Config{Providers: []ProviderConfig{{Host: "a", Port: 563, Enabled: &on}, {Host: "b", Port: 119, Enabled: &off}}}
	d := &Config{Providers: []ProviderConfig{{Host: "a", Port: 563, Enabled: &on}}}
	if c.ProviderFingerprint() != d.ProviderFingerprint() {
		t.Fatal("disabled providers must not count")
	}
	if a.ProviderFingerprint() == d.ProviderFingerprint() {
		t.Fatal("adding a provider must change the fingerprint")
	}
	if len(a.ProviderFingerprint()) != 16 {
		t.Fatalf("fingerprint should be 8 bytes hex, got %q", a.ProviderFingerprint())
	}
}
