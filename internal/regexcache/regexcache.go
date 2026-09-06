// Package regexcache provides a process-wide, size-bounded cache of compiled
// regular expressions.
//
// Regex patterns in AltMount frequently originate from user configuration or
// API request payloads (scoring rules, exclude keywords, custom formats).
// Caching them unboundedly lets unique per-request patterns accumulate forever
// and grow heap usage indefinitely, so the cache resets once it exceeds a cap;
// hot patterns are rebuilt lazily on the next miss.
package regexcache

import (
	"regexp"
	"strings"
	"sync"
)

// maxEntries bounds memory retention. Config-driven patterns number in the
// dozens, so a reset at this size is rare in practice.
const maxEntries = 1024

var (
	mu    sync.Mutex
	cache = make(map[string]*regexp.Regexp)
)

// Get returns the compiled pattern, compiling and caching it on first use.
// Patterns without an explicit case directive are compiled case-insensitive,
// mirroring the historical behavior at each former call site.
// It returns (nil, err) when the pattern does not compile; failed patterns are
// never cached so transient inputs cannot poison the cache.
func Get(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, nil
	}

	mu.Lock()
	defer mu.Unlock()

	if re, ok := cache[pattern]; ok {
		return re, nil
	}

	expr := pattern
	if !strings.HasPrefix(pattern, "(?i)") {
		expr = "(?i)" + pattern
	}
	re, err := regexp.Compile(expr)
	if err != nil {
		return nil, err
	}
	if len(cache) >= maxEntries {
		cache = make(map[string]*regexp.Regexp)
	}
	cache[pattern] = re
	return re, nil
}
