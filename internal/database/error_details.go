package database

import (
	"encoding/json"

	"github.com/javi11/altmount/internal/holes"
)

// HealthErrorDetails is the structured JSON stored in file_health.error_details.
// Both the health checker and the streaming failure path marshal this envelope;
// the frontend parses it to render playback-impact information. Legacy rows may
// contain other ad-hoc JSON shapes or plain strings — parsers must tolerate that.
type HealthErrorDetails struct {
	ErrorType       string        `json:"error_type"`
	Message         string        `json:"message,omitempty"`
	MissingArticles int           `json:"missing_articles,omitempty"`
	TotalArticles   int           `json:"total_articles,omitempty"`
	Sampled         int           `json:"sampled,omitempty"`
	PlaybackImpact  *holes.Impact `json:"playback_impact,omitempty"`

	// UnresolvedSegments counts segments whose availability was never
	// established — transport failures, or ids the sweep never reached.
	// They are deliberately not counted as missing.
	UnresolvedSegments int `json:"unresolved_segments,omitempty"`

	// TerminatedEarly marks a result produced by a check that stopped before
	// examining every planned segment, so Sampled is a partial count and the
	// missing-segment map is incomplete by design.
	TerminatedEarly bool `json:"terminated_early,omitempty"`

	// TerminationReason explains why the check stopped early.
	TerminationReason string `json:"termination_reason,omitempty"`
}

// Marshal renders the envelope for storage, returning nil on the (practically
// impossible) marshal error so callers can assign it directly to *string fields.
func (d *HealthErrorDetails) Marshal() *string {
	data, err := json.Marshal(d)
	if err != nil {
		return nil
	}
	s := string(data)
	return &s
}
