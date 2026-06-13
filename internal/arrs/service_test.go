package arrs

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/config"
	"github.com/stretchr/testify/assert"
)

func TestFindInstanceForFilePath(t *testing.T) {
	// This test is limited because it requires actual Radarr/Sonarr clients or complex mocks
	// But we can at least verify it compiles and handles basic logic if we were to mock the clients.
	// For now, let's just ensure the service can be initialized.

	cfg := &config.Config{
		Arrs: config.ArrsConfig{
			RadarrInstances: []config.ArrsInstanceConfig{
				{
					Name:    "radarr-test",
					URL:     "http://localhost:7878",
					APIKey:  "apikey",
					Enabled: new(true),
				},
			},
		},
	}

	getter := func() *config.Config { return cfg }
	s := NewService(getter, nil, nil, nil)

	assert.NotNil(t, s)
}

func TestService_ClearInstanceCache(t *testing.T) {
	cfg := &config.Config{}
	getter := func() *config.Config { return cfg }
	s := NewService(getter, nil, nil, nil)

	// Call it, making sure it doesn't crash/panic when s.data is populated
	ctx := context.Background()
	s.ClearInstanceCache(ctx, "radarr-test")
	s.ClearInstanceCache(ctx, "") // Empty safe check

	// Nil s.data check
	var sNil *Service = &Service{}
	sNil.ClearInstanceCache(ctx, "radarr-test")
}
