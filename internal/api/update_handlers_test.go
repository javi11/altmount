package api

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsDockerAvailable(t *testing.T) {
	// By default, it should be false in a normal environment unless /var/run/docker.sock exists
	// and docker CLI is in PATH.
	
	// We can't easily mock /var/run/docker.sock without root, but we can check the logic.
	available := isDockerAvailable()
	
	// In most CI environments this will be false.
	// If it's true, it means the environment has docker.
	t.Logf("Docker available in this environment: %v", available)
	
	// Ensure it doesn't panic
	assert.NotPanics(t, func() { isDockerAvailable() })
}

func TestIsDockerAvailable_Mock(t *testing.T) {
    // Create a dummy file for docker.sock in a temp dir
    tmpDir, err := os.MkdirTemp("", "docker-test")
    assert.NoError(t, err)
    defer os.RemoveAll(tmpDir)
    
    // We can't mock /var/run/docker.sock easily because isDockerAvailable has it hardcoded.
    // This shows that isDockerAvailable might be hard to test if we don't allow path injection.
}
