package health

import (
	"math/rand"
	"time"
)

const (
	minInterval              = 1 * time.Hour       // Absolute minimum interval
	aggressiveCheckThreshold = 7 * 24 * time.Hour  // Files younger than 7 days
	dailyCheckThreshold      = 30 * 24 * time.Hour // Files between 7 and 30 days
	aggressiveCheckInterval  = 6 * time.Hour
	dailyCheckInterval       = 24 * time.Hour
	normalCheckInterval      = 90 * 24 * time.Hour // Files older than 30 days
)

// calculateInitialCheck calculates the first check time for a newly discovered file
func calculateInitialCheck(releaseDate time.Time) time.Time {
	// Spread initial checks over the next 24 hours to avoid thundering herd on bulk imports
	jitterMinutes := rand.Intn(1440)
	return time.Now().UTC().Add(time.Duration(jitterMinutes) * time.Minute)
}

// CalculateNextCheck calculates the next check time after a successful health check
// Implements a tiered scheduling strategy based on file age.
func CalculateNextCheck(releaseDate, lastCheck time.Time) time.Time {
	age := lastCheck.Sub(releaseDate) // Age at the time of the last successful check

	var interval time.Duration
	if age < aggressiveCheckThreshold {
		// For very new files, use their age as the interval but cap at 6 hours
		interval = min(age, aggressiveCheckInterval)
	} else if age < dailyCheckThreshold {
		interval = dailyCheckInterval
	} else {
		interval = normalCheckInterval
		// Add jitter of +/- 7 days (approx 10%) to spread out bulk loads
		jitterDays := rand.Intn(14) - 7
		interval += time.Duration(jitterDays) * 24 * time.Hour
	}

	// Ensure the interval is at least the absolute minimum
	if interval < minInterval {
		interval = minInterval
	}

	return lastCheck.Add(interval)
}
