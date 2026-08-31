package instances

import (
	"errors"
	"net"
	"testing"
)

func fakeResolver(hosts map[string][]net.IP) func(string) ([]net.IP, error) {
	return func(host string) ([]net.IP, error) {
		if ips, ok := hosts[host]; ok {
			return ips, nil
		}
		return nil, errors.New("no such host")
	}
}

func TestValidateInternalARRURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		hosts   map[string][]net.IP
		wantErr bool
	}{
		// Allowed: internal targets
		{name: "private 192.168 literal", url: "http://192.168.1.10:8989", wantErr: false},
		{name: "private 10.x literal", url: "http://10.0.0.5:7878", wantErr: false},
		{name: "private 172.16 literal", url: "http://172.16.0.3:8989", wantErr: false},
		{name: "loopback literal", url: "http://127.0.0.1:8989", wantErr: false},
		{name: "ipv6 ULA literal", url: "http://[fd00::1]:8989", wantErr: false},
		{name: "ipv6 loopback literal", url: "http://[::1]:8989", wantErr: false},
		{name: "docker hostname resolves private", url: "http://sonarr:8989",
			hosts: map[string][]net.IP{"sonarr": {net.ParseIP("172.18.0.4")}}, wantErr: false},
		{name: "localhost resolves loopback", url: "http://localhost:8989",
			hosts: map[string][]net.IP{"localhost": {net.ParseIP("127.0.0.1")}}, wantErr: false},

		// Rejected: external / dangerous targets
		{name: "public ip literal", url: "http://8.8.8.8", wantErr: true},
		{name: "cloud metadata link-local", url: "http://169.254.169.254", wantErr: true},
		{name: "public hostname", url: "http://evil.example.com",
			hosts: map[string][]net.IP{"evil.example.com": {net.ParseIP("93.184.216.34")}}, wantErr: true},
		{name: "dns rebinding mixed public+private", url: "http://rebind.test",
			hosts: map[string][]net.IP{"rebind.test": {net.ParseIP("192.168.1.5"), net.ParseIP("93.184.216.34")}}, wantErr: true},
		{name: "unresolvable hostname", url: "http://nope.invalid", wantErr: true},
		{name: "empty url", url: "", wantErr: true},
		{name: "no host", url: "http://", wantErr: true},
		{name: "invalid url", url: "://bad", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateInternalARRURL(tt.url, fakeResolver(tt.hosts))
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q, got nil", tt.url)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("expected no error for %q, got %v", tt.url, err)
			}
		})
	}
}

func TestIsInternalIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},
		{"10.1.2.3", true},
		{"172.16.5.5", true},
		{"192.168.0.1", true},
		{"fd00::1", true},
		{"::1", true},
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"172.32.0.1", false},      // just outside 172.16/12
	}
	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isInternalIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Fatalf("isInternalIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}
