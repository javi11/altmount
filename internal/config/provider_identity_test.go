package config

import (
	"strings"
	"testing"
)

func TestProviderIdentityUsesStableID(t *testing.T) {
	provider := ProviderConfig{
		ID:       "provider-primary",
		Host:     "news.example.test",
		Port:     563,
		Username: "synthetic-account",
	}

	if got := provider.NNTPPoolName(); got != provider.ID {
		t.Fatalf("NNTPPoolName() = %q, want stable ID %q", got, provider.ID)
	}

	poolProvider := provider.ToNNTPProvider()
	if poolProvider.Name != provider.ID {
		t.Fatalf("ToNNTPProvider().Name = %q, want stable ID %q", poolProvider.Name, provider.ID)
	}
	if strings.Contains(poolProvider.Name, provider.Username) {
		t.Fatalf("pool provider name contains the authentication username")
	}
}

func TestProviderIdentityKeepsSameHostAccountsDistinct(t *testing.T) {
	first := ProviderConfig{ID: "provider-first", Host: "news.example.test", Port: 563, Username: "first-account"}
	second := ProviderConfig{ID: "provider-second", Host: "news.example.test", Port: 563, Username: "second-account"}

	if first.NNTPPoolName() == second.NNTPPoolName() {
		t.Fatal("same-host providers must retain distinct stable IDs")
	}
	if first.ToNNTPProvider().Name == second.ToNNTPProvider().Name {
		t.Fatal("same-host nntppool providers must retain distinct stable IDs")
	}
}

func TestConfigValidateRequiresUniqueProviderIDs(t *testing.T) {
	validProvider := func(id string) ProviderConfig {
		return ProviderConfig{ID: id, Host: "news.example.test", Port: 563, MaxConnections: 1}
	}

	t.Run("rejects empty ID", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Providers = []ProviderConfig{validProvider("")}
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted an empty provider ID")
		}
	})

	t.Run("rejects duplicate IDs", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Providers = []ProviderConfig{validProvider("provider-duplicate"), validProvider("provider-duplicate")}
		if err := cfg.Validate(); err == nil {
			t.Fatal("Validate() accepted duplicate provider IDs")
		}
	})

	for name, id := range map[string]string{
		"leading whitespace":  " provider",
		"trailing whitespace": "provider ",
		"newline":             "provider\nline",
		"line separator":      "provider\u2028line",
		"paragraph separator": "provider\u2029line",
		"bidi control":        "provider\u202Eline",
	} {
		t.Run("rejects "+name, func(t *testing.T) {
			cfg := DefaultConfig()
			cfg.Providers = []ProviderConfig{validProvider(id)}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() accepted unsafe provider ID")
			}
		})
	}
}
