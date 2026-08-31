package scanner

import (
	"context"
	"testing"

	"github.com/javi11/altmount/internal/arrs/clients"
	"github.com/javi11/altmount/internal/arrs/instances"
	"github.com/javi11/altmount/internal/config"
)

func TestFindInstanceForFilePath_CategoryMatch(t *testing.T) {
	enabled := true

	tests := []struct {
		name        string
		completeDir string
		filePath    string
		radarrCat   string
		sonarrCat   string
		wantType    string
		wantName    string
		wantErr     bool
	}{
		{
			name:        "completeDir set to /complete, matches radarr by category moviesHQ",
			completeDir: "/complete",
			filePath:    "/mnt/disk1/cloud/downloads/complete/moviesHQ/The.Devil.Wears.Prada.2006.2160p/The.Devil...mkv",
			radarrCat:   "moviesHQ",
			sonarrCat:   "tvHQ",
			wantType:    "radarr",
			wantName:    "radarr_instance",
		},
		{
			name:        "completeDir set to /complete, matches sonarr by category tvHQ",
			completeDir: "/complete",
			filePath:    "/mnt/disk1/cloud/downloads/complete/tvHQ/Abbott.Elementary/Abbott...mkv",
			radarrCat:   "moviesHQ",
			sonarrCat:   "tvHQ",
			wantType:    "sonarr",
			wantName:    "sonarr_instance",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{
				MountPath: "/mnt/disk1/cloud/downloads",
				SABnzbd: config.SABnzbdConfig{
					CompleteDir: tt.completeDir,
				},
				Arrs: config.ArrsConfig{
					RadarrInstances: []config.ArrsInstanceConfig{
						{
							Name:     "radarr_instance",
							Category: tt.radarrCat,
							Enabled:  &enabled,
							URL:      "http://localhost:7878",
						},
					},
					SonarrInstances: []config.ArrsInstanceConfig{
						{
							Name:     "sonarr_instance",
							Category: tt.sonarrCat,
							Enabled:  &enabled,
							URL:      "http://localhost:8989",
						},
					},
				},
			}

			configGetter := func() *config.Config {
				return cfg
			}

			instMgr := instances.NewManager(configGetter, nil)
			clientMgr := clients.NewManager(nil)
			mgr := NewManager(configGetter, instMgr, clientMgr, nil, nil, nil)

			gotType, gotName, err := mgr.findInstanceForFilePath(context.Background(), tt.filePath, "")
			if (err != nil) != tt.wantErr {
				t.Fatalf("unexpected error result: %v", err)
			}

			if gotType != tt.wantType {
				t.Errorf("gotType = %q; want %q", gotType, tt.wantType)
			}
			if gotName != tt.wantName {
				t.Errorf("gotName = %q; want %q", gotName, tt.wantName)
			}
		})
	}
}
