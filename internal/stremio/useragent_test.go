package stremio

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUserAgentManager_GetDefaultUserAgent(t *testing.T) {
	mgr := NewUserAgentManager(nil)

	tvUA := mgr.GetUserAgent("series", "")
	assert.Contains(t, tvUA, "Sonarr/")

	movieUA := mgr.GetUserAgent("movie", "")
	assert.Contains(t, movieUA, "Radarr/")

	customUA := mgr.GetUserAgent("movie", "CustomApp/1.0")
	assert.Equal(t, "CustomApp/1.0", customUA)
}

func TestUserAgentManager_CheckLocalARRs(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "testapikey", r.Header.Get("X-Api-Key"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"appName": "Sonarr",
			"version": "4.2.0.1000",
			"osName": "alpine",
			"osVersion": "3.23.3"
		}`))
	}))
	defer ts.Close()

	mgr := NewUserAgentManager(ts.Client())
	ok := mgr.CheckLocalARRs(context.Background(), ts.URL, "testapikey", "", "")
	assert.True(t, ok)

	info := mgr.GetInfo()
	assert.Equal(t, "4.2.0.1000", info.SonarrVersion)
	assert.Equal(t, "Sonarr/4.2.0.1000 (alpine 3.23.3)", info.TVUserAgent)
	assert.Equal(t, "local_instance", info.Source)
}
