package health

import (
	"time"
)

// calculateInitialCheck calculates the first check time for a newly discovered file
// Uses the formula: NextCheck = ReleaseDate + 2 * (NOW - ReleaseDate)
// with a minimum interval of 1 hour
func calculateInitialCheck(releaseDate time.Time) time.Time {
	now := time.Now()
	age := now.Sub(releaseDate)

	// Enforce minimum interval of 1 hour
	if age < 1*time.Hour {
		age = 1 * time.Hour
	}

	// NextCheck = ReleaseDate + 2 * (NOW - ReleaseDate)
	return releaseDate.Add(2 * age)
}

// calculateNextCheck calculates the next check time after a successful health check
// Uses the exponential backoff formula: NextCheck = ReleaseDate + 2 * (LastCheck - ReleaseDate)
// with a minimum interval of 1 hour
func calculateNextCheck(releaseDate, lastCheck time.Time) time.Time {
	timeSinceRelease := lastCheck.Sub(releaseDate)

	// Enforce minimum interval of 1 hour
	if timeSinceRelease < 1*time.Hour {
		timeSinceRelease = 1 * time.Hour
	}

	// NextCheck = ReleaseDate + 2 * (LastCheck - ReleaseDate)
	return releaseDate.Add(2 * timeSinceRelease)
}
