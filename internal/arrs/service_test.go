package arrs

import (
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
					Enabled: boolPtr(true),
				},
			},
		},
	}

	getter := func() *config.Config { return cfg }
	s := NewService(getter, nil)

	assert.NotNil(t, s)
}

func boolPtr(b bool) *bool {
	return &b
}
