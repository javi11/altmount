package health

import (
	"time"
)

const (
	minInterval             = 1 * time.Hour // Absolute minimum interval
	aggressiveCheckThreshold = 7 * 24 * time.Hour // Files younger than 7 days
	dailyCheckThreshold     = 30 * 24 * time.Hour // Files between 7 and 30 days
	aggressiveCheckInterval = 6 * time.Hour
	dailyCheckInterval      = 24 * time.Hour
	normalCheckInterval     = 90 * 24 * time.Hour // Files older than 30 days
)

// calculateInitialCheck calculates the first check time for a newly discovered file
func calculateInitialCheck(releaseDate time.Time) time.Time {
	// Always schedule initial check immediately (with a small buffer)
	return time.Now().UTC().Add(1 * time.Minute)
}

// CalculateNextCheck calculates the next check time after a successful health check
// Implements a tiered scheduling strategy based on file age.
func CalculateNextCheck(releaseDate, lastCheck time.Time) time.Time {
	age := lastCheck.Sub(releaseDate) // Age at the time of the last successful check

	var interval time.Duration
	if age < aggressiveCheckThreshold {
		// For very new files, use their age as the interval but cap at 6 hours
		if age < aggressiveCheckInterval {
			interval = age
		} else {
			interval = aggressiveCheckInterval
		}
	} else if age < dailyCheckThreshold {
		interval = dailyCheckInterval
	} else {
		interval = normalCheckInterval
	}

	// Ensure the interval is at least the absolute minimum
	if interval < minInterval {
		interval = minInterval
	}

	return lastCheck.Add(interval)
}
