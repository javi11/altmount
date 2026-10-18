package slogutil

import (
	"fmt"
	"log/slog"
	"regexp"
)

// secretQueryParam matches query parameters that carry credentials: the
// download key in stream URLs (a bearer secret for every file) and indexer
// API keys in download URLs.
var secretQueryParam = regexp.MustCompile(`(?i)\b(download_key|api_?key)=[^&\s"'<>]+`)

// RedactSecrets replaces credential values in URL query strings with
// "REDACTED".
func RedactSecrets(s string) string {
	return secretQueryParam.ReplaceAllString(s, "${1}=REDACTED")
}

// redactAttr keeps credentials out of the logs: log lines routinely include
// stream or download URLs, in attributes or inside error messages.
func redactAttr(_ []string, a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		if s := a.Value.String(); secretQueryParam.MatchString(s) {
			return slog.String(a.Key, RedactSecrets(s))
		}
	case slog.KindAny:
		var s string
		switch v := a.Value.Any().(type) {
		case error:
			s = v.Error()
		case fmt.Stringer:
			s = v.String()
		default:
			return a
		}
		if secretQueryParam.MatchString(s) {
			return slog.String(a.Key, RedactSecrets(s))
		}
	}
	return a
}
