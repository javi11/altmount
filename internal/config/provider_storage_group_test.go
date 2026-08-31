package config

import "testing"

// baseProvider returns a minimal, valid ProviderConfig for equality tests.
func baseProvider() ProviderConfig {
	return ProviderConfig{
		ID:             "p1",
		Host:           "news.newshosting.com",
		Port:           563,
		Username:       "user",
		Password:       "pass",
		MaxConnections: 10,
	}
}

func TestProvidersFieldsEqual_StorageGroup(t *testing.T) {
	tests := []struct {
		name string
		a    ProviderConfig
		b    ProviderConfig
		want bool
	}{
		{
			name: "both empty storage group are equal",
			a:    baseProvider(),
			b:    baseProvider(),
			want: true,
		},
		{
			name: "same storage group are equal",
			a: func() ProviderConfig { p := baseProvider(); p.StorageGroup = "omicron"; return p }(),
			b: func() ProviderConfig { p := baseProvider(); p.StorageGroup = "omicron"; return p }(),
			want: true,
		},
		{
			name: "different storage group are not equal",
			a: func() ProviderConfig { p := baseProvider(); p.StorageGroup = "omicron"; return p }(),
			b: func() ProviderConfig { p := baseProvider(); p.StorageGroup = "highwinds"; return p }(),
			want: false,
		},
		{
			name: "adding a storage group is not equal",
			a:    baseProvider(),
			b: func() ProviderConfig { p := baseProvider(); p.StorageGroup = "omicron"; return p }(),
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := providersFieldsEqual(tt.a, tt.b); got != tt.want {
				t.Errorf("providersFieldsEqual() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToNNTPProvider_StorageGroup(t *testing.T) {
	p := baseProvider()
	p.StorageGroup = "omicron"

	got := p.ToNNTPProvider()
	if got.StorageGroup != "omicron" {
		t.Errorf("ToNNTPProvider().StorageGroup = %q, want %q", got.StorageGroup, "omicron")
	}
}
