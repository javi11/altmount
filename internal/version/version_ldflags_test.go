package version

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDockerfilesInjectVersionLdflags guards against the regression from
// issue #780, where docker/Dockerfile.ci built the binary without the
// version ldflags. That left version.Version at its "dev" default in every
// published image (including :latest), so the Updates panel always reported
// the running version as "dev".
//
// Any Dockerfile that builds the Go binary must inject all three version
// symbols so the running binary reports its real release version.
func TestDockerfilesInjectVersionLdflags(t *testing.T) {
	symbols := []string{
		"github.com/javi11/altmount/internal/version.Version=",
		"github.com/javi11/altmount/internal/version.GitCommit=",
		"github.com/javi11/altmount/internal/version.Timestamp=",
	}

	dockerfiles := []string{
		filepath.Join("..", "..", "docker", "Dockerfile"),
		filepath.Join("..", "..", "docker", "Dockerfile.ci"),
	}

	for _, df := range dockerfiles {
		t.Run(df, func(t *testing.T) {
			data, err := os.ReadFile(df)
			if err != nil {
				t.Fatalf("failed to read %s: %v", df, err)
			}
			content := string(data)
			for _, sym := range symbols {
				if !strings.Contains(content, sym) {
					t.Errorf("%s does not inject version ldflag %q; the built binary would report the default %q version", df, sym, Version)
				}
			}
		})
	}
}
