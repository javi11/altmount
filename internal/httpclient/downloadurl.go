package httpclient

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// ValidateDownloadURL checks that a URL supplied by a remote indexer is safe to
// fetch. Indexer search responses drive these requests, so a compromised or
// hostile indexer would otherwise be able to point AltMount at loopback,
// link-local or private addresses — including cloud instance-metadata
// endpoints — turning the download path into an SSRF primitive.
//
// Hostnames are not resolved here: a DNS name that resolves to a private
// address still passes. Blocking literal private targets removes the direct
// attack while keeping the check cheap and dependency-free on a hot path;
// callers additionally refuse redirects, so a remote host cannot bounce the
// request onto an internal address.
func ValidateDownloadURL(raw string) error {
	if strings.TrimSpace(raw) == "" {
		return fmt.Errorf("download URL is empty")
	}

	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("download URL is not parseable: %w", err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return fmt.Errorf("download URL scheme %q is not allowed", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("download URL has no host")
	}

	// Reject literal IPs that point back into the local network.
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("download URL host %q is a private address", host)
		}
		return nil
	}

	// "localhost" and friends never legitimately serve indexer downloads.
	if lower := strings.ToLower(host); lower == "localhost" || strings.HasSuffix(lower, ".localhost") {
		return fmt.Errorf("download URL host %q is a private address", host)
	}

	return nil
}
