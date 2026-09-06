package httpclient

import (
	"errors"
	"fmt"
	"net"
	"net/http"
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
// callers validate redirect hops using SafeDownloadCheckRedirect, so a remote
// host cannot bounce the request onto an internal address.
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

// SafeDownloadCheckRedirect returns a CheckRedirect function that permits redirects
// up to maxRedirects (default 10) and ensures each redirect target satisfies
// ValidateDownloadURL. It also redacts sensitive credentials (X-Api-Key, Authorization)
// when redirecting across different hostnames.
func SafeDownloadCheckRedirect(maxRedirects int) func(req *http.Request, via []*http.Request) error {
	if maxRedirects <= 0 {
		maxRedirects = 10
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= maxRedirects {
			return fmt.Errorf("stopped after %d redirects", maxRedirects)
		}
		if err := ValidateDownloadURL(req.URL.String()); err != nil {
			return fmt.Errorf("refusing redirect: %w", err)
		}
		if len(via) > 0 {
			prev := via[len(via)-1]
			if !strings.EqualFold(prev.URL.Hostname(), req.URL.Hostname()) {
				req.Header.Del("X-Api-Key")
				req.Header.Del("Authorization")
			}
		}
		return nil
	}
}

// RedactURLError strips the query string, fragment and userinfo from the URL
// embedded in a *url.Error before it can reach a log or an API response.
//
// Indexer requests carry credentials as query parameters — an `apikey` on
// Newznab search/caps calls, and whatever the upstream indexer embeds in a
// Prowlarr-issued download URL. net/http reports transport failures as a
// *url.Error whose message is the full request URL, so wrapping such an error
// unmodified publishes the credential wherever the caller logs or renders it.
//
// The wrapped cause is preserved, so errors.Is/As against the underlying error
// (context.DeadlineExceeded, net.Error, ...) behave as before. Scheme, host and
// path are kept — they are what makes the error diagnosable.
func RedactURLError(err error) error {
	if err == nil {
		return nil
	}

	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}

	if parsed, parseErr := url.Parse(urlErr.URL); parseErr == nil {
		parsed.RawQuery = ""
		parsed.Fragment = ""
		parsed.User = nil
		urlErr.URL = parsed.String()
	} else {
		// Unparseable URL: drop it wholesale rather than risk leaking a
		// fragment of the query string.
		urlErr.URL = "[redacted]"
	}

	return err
}
