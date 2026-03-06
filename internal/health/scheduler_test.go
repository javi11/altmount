package health

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestCalculateInitialCheckForNewFile_WithinFiveMinutes(t *testing.T) {
	before := time.Now().UTC()

	result := calculateInitialCheckForNewFile()

	maxAllowed := time.Now().UTC().Add(5 * time.Minute)

	assert.True(t, !result.Before(before.Add(-time.Second)),
		"scheduled time should not be before now")
	assert.True(t, result.Before(maxAllowed.Add(time.Second)),
		"scheduled time should be within 5 minutes of now, got %v", result)
}

func TestCalculateInitialCheckForNewFile_MuchSoonerThanLibrarySync(t *testing.T) {
	// Library sync uses calculateInitialCheck() with up to 1440 minutes of jitter.
	// New file imports must always be scheduled within 5 minutes.
	const libraryJitterMinutes = 1440
	const newFileMaxJitterMinutes = 5

	assert.Less(t, newFileMaxJitterMinutes, libraryJitterMinutes,
		"new file max jitter must be smaller than library sync jitter")

	// Run many iterations: none should exceed 5 minutes from now.
	now := time.Now().UTC()
	for i := range 200 {
		result := calculateInitialCheckForNewFile()
		diff := result.Sub(now)
		assert.LessOrEqualf(t, diff, 5*time.Minute+time.Second,
			"iteration %d: scheduled time exceeded 5-minute bound (diff=%v)", i, diff)
	}
}
