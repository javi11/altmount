package httpclient

import (
	"net/http"
	"net/url"

	"golang.org/x/net/http/httpproxy"
)

// buildProxyFunc returns an http.Transport.Proxy function honouring the given
// http/https/no proxy values. Empty values mean "do not proxy that scheme".
// It does NOT consult environment variables — config is authoritative.
func buildProxyFunc(httpProxy, httpsProxy, noProxy string) func(*http.Request) (*url.URL, error) {
	if httpProxy == "" && httpsProxy == "" {
		return func(*http.Request) (*url.URL, error) { return nil, nil }
	}
	cfg := &httpproxy.Config{
		HTTPProxy:  httpProxy,
		HTTPSProxy: httpsProxy,
		NoProxy:    noProxy,
	}
	proxyFn := cfg.ProxyFunc()
	return func(r *http.Request) (*url.URL, error) {
		return proxyFn(r.URL)
	}
}

// WithProxyConfig returns an Option that installs a proxy-aware
// *http.Transport derived from the supplied values. Pass empty strings to
// disable proxying. If a Transport was already set by an earlier option, the
// proxy function is layered on top of a clone of that transport.
func WithProxyConfig(httpProxy, httpsProxy, noProxy string) Option {
	return func(o *Options) {
		var base *http.Transport
		if o.Transport != nil {
			base = o.Transport.Clone()
		} else if dt, ok := http.DefaultTransport.(*http.Transport); ok {
			base = dt.Clone()
		} else {
			base = &http.Transport{}
		}
		base.Proxy = buildProxyFunc(httpProxy, httpsProxy, noProxy)
		o.Transport = base
	}
}
