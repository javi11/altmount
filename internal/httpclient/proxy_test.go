package httpclient

import (
	"net/http"
	"testing"
)

func TestBuildProxyFunc_AllEmpty(t *testing.T) {
	fn := buildProxyFunc("", "", "")
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	u, err := fn(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil proxy URL, got %v", u)
	}
}

func TestBuildProxyFunc_HTTPProxy(t *testing.T) {
	fn := buildProxyFunc("http://proxy:3128", "", "")
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	u, err := fn(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if u == nil || u.Host != "proxy:3128" {
		t.Fatalf("expected proxy:3128, got %v", u)
	}
}

func TestBuildProxyFunc_HTTPSProxy(t *testing.T) {
	fn := buildProxyFunc("", "http://proxy:3128", "")
	req, _ := http.NewRequest("GET", "https://example.com", nil)
	u, err := fn(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if u == nil || u.Host != "proxy:3128" {
		t.Fatalf("expected proxy:3128, got %v", u)
	}
}

func TestBuildProxyFunc_NoProxyMatch(t *testing.T) {
	fn := buildProxyFunc("http://proxy:3128", "http://proxy:3128", "internal.local,10.0.0.0/8")
	req, _ := http.NewRequest("GET", "http://internal.local/x", nil)
	u, err := fn(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil (bypass), got %v", u)
	}
}

func TestWithProxyConfig_AppliesTransport(t *testing.T) {
	c := New(WithProxyConfig("http://proxy:3128", "", ""))
	if c.Transport == nil {
		t.Fatalf("expected transport set")
	}
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport")
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	u, _ := tr.Proxy(req)
	if u == nil || u.Host != "proxy:3128" {
		t.Fatalf("proxy not applied: %v", u)
	}
}

func TestWithProxyConfig_EmptyDisables(t *testing.T) {
	c := New(WithProxyConfig("", "", ""))
	tr, ok := c.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected *http.Transport")
	}
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	u, err := tr.Proxy(req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil proxy (disabled), got %v", u)
	}
}
