package database

import (
	"encoding/json"

	"github.com/javi11/altmount/internal/mediaprobe"
)

// HealthErrorDetails is the structured JSON stored in file_health.error_details.
// Both the health checker and the streaming failure path marshal this envelope;
// the frontend parses it to render playback-impact information. Legacy rows may
// contain other ad-hoc JSON shapes or plain strings — parsers must tolerate that.
type HealthErrorDetails struct {
	ErrorType       string                     `json:"error_type"`
	Message         string                     `json:"message,omitempty"`
	MissingArticles int                        `json:"missing_articles,omitempty"`
	TotalArticles   int                        `json:"total_articles,omitempty"`
	Sampled         int                        `json:"sampled,omitempty"`
	PlaybackImpact  *mediaprobe.Classification `json:"playback_impact,omitempty"`
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
