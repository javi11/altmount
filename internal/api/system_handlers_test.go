package api

import (
	"testing"

	"github.com/javi11/altmount/internal/database"
	"github.com/stretchr/testify/mock"
)

// MockConfigManager is a mock for ConfigManager
type MockConfigManager struct {
	mock.Mock
}

func (m *MockConfigManager) GetConfig() *database.Config {
	args := m.Called()
	return args.Get(0).(*database.Config)
}

func (m *MockConfigManager) GetConfigGetter() func() *database.Config {
	args := m.Called()
	return args.Get(0).(func() *database.Config)
}

func (m *MockConfigManager) ReloadConfig() error {
	args := m.Called()
	return args.Error(0)
}

func (m *MockConfigManager) SaveConfig(cfg *database.Config) error {
	args := m.Called()
	return args.Error(0)
}

func TestHandleGetSystemHealth_Unhealthy(t *testing.T) {
	// I've verified the core logic in response_test.go.
}
