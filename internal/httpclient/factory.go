package httpclient

import (
	"net/http"
	"time"
)

// NetworkProxyConfig is the minimal interface the factory needs from the
// application config. Implemented by config.NetworkConfig — declared here
// to avoid an internal/config ↔ internal/httpclient import cycle.
type NetworkProxyConfig interface {
	GetHTTPProxy() string
	GetHTTPSProxy() string
	GetNoProxy() string
}

// NewForExternal builds a proxy-aware *http.Client with the given timeout,
// reading proxy settings from the supplied config. Used by every external
// integration (Prowlarr, SABnzbd, NZBLNK resolver, Arrs). Internal endpoints
// should NOT use this — call New() or use http.DefaultClient instead.
//
// A nil net argument disables proxying.
func NewForExternal(net NetworkProxyConfig, timeout time.Duration) *http.Client {
	if net == nil {
		return New(WithTimeout(timeout))
	}
	return New(
		WithTimeout(timeout),
		WithProxyConfig(net.GetHTTPProxy(), net.GetHTTPSProxy(), net.GetNoProxy()),
	)
}
