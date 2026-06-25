// Package streamcheck provides an on-demand, import-free availability check for
// an NZB's Usenet segments. It parses an NZB in memory, samples a subset of its
// segments, and issues NNTP STAT round-trips through the shared connection pool
// to decide whether the release is still streamable. It writes nothing to the
// metadata store or the import queue — it is a cheap pre-check that clients
// (e.g. AIOStreams) call to filter dead or incomplete releases before listing.
package streamcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/javi11/altmount/internal/config"
	metapb "github.com/javi11/altmount/internal/metadata/proto"
	"github.com/javi11/altmount/internal/pool"
	"github.com/javi11/altmount/internal/usenet"
	"github.com/javi11/nzbparser"
)

// Verdict is the outcome of an availability check.
type Verdict string

const (
	// VerdictAvailable means every sampled segment was reachable.
	VerdictAvailable Verdict = "available"
	// VerdictDegraded means some sampled segments were missing but within the
	// configured acceptable threshold (e.g. potentially PAR2-recoverable).
	VerdictDegraded Verdict = "degraded"
	// VerdictDead means too many sampled segments were missing (or the NZB was
	// unparseable / empty) — the release is not streamable.
	VerdictDead Verdict = "dead"
	// VerdictUnknown means the check could not be completed (e.g. no connection
	// pool). Callers should fail open and treat the release as available.
	VerdictUnknown Verdict = "unknown"
)

// Result holds the outcome of a single NZB availability check.
type Result struct {
	Verdict    Verdict `json:"verdict"`
	Checked    int     `json:"checked"`
	Missing    int     `json:"missing"`
	MissingPct float64 `json:"missing_pct"`
	Cached     bool    `json:"cached"`
}

// Checker verifies NZB segment availability against the Usenet connection pool.
type Checker struct {
	poolManager  pool.Manager
	configGetter config.ConfigGetter
	cache        *verdictCache
}

// NewChecker creates a Checker. configGetter is read on every call so live
// configuration changes (sampling, timeout, threshold) take effect immediately.
func NewChecker(poolManager pool.Manager, configGetter config.ConfigGetter) *Checker {
	return &Checker{
		poolManager:  poolManager,
		configGetter: configGetter,
		cache:        newVerdictCache(),
	}
}

// Check parses nzbData in memory and samples segment availability via NNTP STAT.
// It performs no import. An unparseable or segment-less NZB returns VerdictDead;
// an unavailable pool or STAT infrastructure error returns VerdictUnknown with a
// non-nil error so callers can fail open.
func (ck *Checker) Check(ctx context.Context, nzbData []byte) (Result, error) {
	cfg := ck.configGetter()

	parsed, err := nzbparser.Parse(bytes.NewReader(nzbData))
	if err != nil {
		// Nothing parseable to stream — treat as dead, not an error.
		return Result{Verdict: VerdictDead}, nil
	}

	segments := collectDataSegments(parsed)
	if len(segments) == 0 {
		return Result{Verdict: VerdictDead}, nil
	}

	fp := fingerprint(segments)
	ttl := cfg.GetStreamCheckCacheTTL()
	if ttl > 0 {
		if cached, ok := ck.cache.get(fp); ok {
			cached.Cached = true
			return cached, nil
		}
	}

	if ck.poolManager == nil || !ck.poolManager.HasPool() {
		return Result{Verdict: VerdictUnknown}, fmt.Errorf("usenet connection pool unavailable")
	}

	validation, err := usenet.ValidateSegmentAvailabilityDetailed(
		ctx,
		segments,
		ck.poolManager,
		cfg.GetStreamCheckMaxConnections(),
		cfg.GetStreamCheckSamplePercentage(),
		nil, // no progress callback
		cfg.GetStreamCheckTimeout(),
	)
	if err != nil {
		// Infrastructure failure — do not poison the cache; fail open as unknown.
		return Result{Verdict: VerdictUnknown}, err
	}

	result := Result{
		Checked: validation.TotalChecked,
		Missing: validation.MissingCount,
	}
	if validation.TotalChecked > 0 {
		result.MissingPct = float64(validation.MissingCount) / float64(validation.TotalChecked) * 100
	}

	switch {
	case validation.MissingCount == 0:
		result.Verdict = VerdictAvailable
	case result.MissingPct <= cfg.GetStreamCheckAcceptableMissingPercentage():
		result.Verdict = VerdictDegraded
	default:
		result.Verdict = VerdictDead
	}

	if ttl > 0 {
		ck.cache.set(fp, result, ttl)
	}

	return result, nil
}

// collectDataSegments flattens every non-PAR2 file's segments into the
// SegmentData slice the validator consumes. Only the message ID is needed: the
// availability check issues NNTP STAT, which reads neither offsets nor sizes.
func collectDataSegments(n *nzbparser.Nzb) []*metapb.SegmentData {
	var segments []*metapb.SegmentData
	for i := range n.Files {
		f := n.Files[i]
		if isPar2(f.Filename) {
			continue
		}
		for _, s := range f.Segments {
			if s.ID == "" {
				continue
			}
			segments = append(segments, &metapb.SegmentData{Id: s.ID})
		}
	}
	return segments
}

func isPar2(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".par2")
}

// fingerprint derives a stable cache key from the release's segment identity.
// Message IDs reference the same Usenet articles regardless of which indexer
// produced the NZB, so the same release dedups across indexers.
func fingerprint(segments []*metapb.SegmentData) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%d", segments[0].Id, segments[len(segments)-1].Id, len(segments))
	return hex.EncodeToString(h.Sum(nil)[:16])
}
