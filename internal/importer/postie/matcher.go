package postie

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/nzbparser"
)

// Matcher matches Postie-generated NZBs back to original queue items
type Matcher struct {
	queueRepo *database.QueueRepository
	config    *config.PostieConfig
	log       *slog.Logger
}

// NewMatcher creates a new Postie matcher
func NewMatcher(queueRepo *database.QueueRepository, configGetter config.ConfigGetter) *Matcher {
	return &Matcher{
		queueRepo: queueRepo,
		config:    configGetter().Postie,
		log:       slog.Default().With("component", "postie-matcher"),
	}
}

// MatchCandidate represents a queue item that could match an incoming NZB
type MatchCandidate struct {
	Item          *database.ImportQueueItem
	Score         int    // Higher is better
	FilenameMatch bool
	CategoryMatch bool
	AgeScore      int
	SizeDiff      int64 // Absolute difference in bytes
}

// FindOriginalItem finds the original queue item for a Postie-generated NZB
// Returns the highest scoring candidate or nil if no match found
func (m *Matcher) FindOriginalItem(ctx context.Context, nzbPath string) (*database.ImportQueueItem, error) {
	// Get all pending Postie items
	pendingItems, err := m.queueRepo.GetPendingPostieItems(ctx)
	if err != nil {
		return nil, err
	}

	if len(pendingItems) == 0 {
		m.log.DebugContext(ctx, "No pending Postie items found", "nzb_path", nzbPath)
		return nil, nil
	}

	// Parse the incoming NZB to extract filename and size
	nzbFilename, nzbSize, err := m.parseNZBInfo(ctx, nzbPath)
	if err != nil {
		m.log.WarnContext(ctx, "Failed to parse NZB for matching", "nzb_path", nzbPath, "error", err)
		return nil, err
	}

	m.log.DebugContext(ctx, "Parsed Postie NZB for matching",
		"nzb_path", nzbPath,
		"filename", nzbFilename,
		"size", nzbSize,
		"candidates", len(pendingItems))

	// Score each candidate
	candidates := make([]*MatchCandidate, 0, len(pendingItems))
	now := time.Now()

	for _, item := range pendingItems {
		candidate := &MatchCandidate{
			Item:     item,
			Score:    0,
			SizeDiff: 0,
		}

		// Filename similarity check (primary matching)
		if item.OriginalReleaseName != nil {
			candidate.FilenameMatch = m.filenamesMatch(*item.OriginalReleaseName, nzbFilename)
			if candidate.FilenameMatch {
				candidate.Score += 100 // High weight for filename match
			}
		}

		// Category match check
		if item.Category != nil {
			candidate.CategoryMatch = m.categoryMatches(*item.Category, nzbPath)
			if candidate.CategoryMatch {
				candidate.Score += 20
			}
		}

		// Age score (prefer items that were marked pending recently)
		ageHours := int(now.Sub(item.UpdatedAt).Hours())
		if ageHours < 1 {
			candidate.AgeScore = 30
		} else if ageHours < 6 {
			candidate.AgeScore = 20
		} else if ageHours < 24 {
			candidate.AgeScore = 10
		}
		candidate.Score += candidate.AgeScore

		// Size proximity (will be used as tiebreaker)
		if item.FileSize != nil && nzbSize > 0 {
			candidate.SizeDiff = abs(*item.FileSize - nzbSize)
		}

		candidates = append(candidates, candidate)
	}

	// Find best match
	var bestMatch *MatchCandidate
	for _, candidate := range candidates {
		// Only consider items with some positive score (filename match or recent age)
		if candidate.Score <= 0 {
			continue
		}

		if bestMatch == nil {
			bestMatch = candidate
			continue
		}

		// Higher score wins
		if candidate.Score > bestMatch.Score {
			bestMatch = candidate
			continue
		}

		// If scores are equal, use size as tiebreaker
		if candidate.Score == bestMatch.Score && candidate.SizeDiff < bestMatch.SizeDiff {
			bestMatch = candidate
		}
	}

	if bestMatch != nil {
		m.log.InfoContext(ctx, "Found match for Postie NZB",
			"nzb_path", nzbPath,
			"queue_id", bestMatch.Item.ID,
			"original_name", bestMatch.Item.OriginalReleaseName,
			"score", bestMatch.Score,
			"size_diff", bestMatch.SizeDiff)
		return bestMatch.Item, nil
	}

	m.log.DebugContext(ctx, "No match found for Postie NZB", "nzb_path", nzbPath)
	return nil, nil
}

// parseNZBInfo extracts the filename and total size from an NZB file
func (m *Matcher) parseNZBInfo(ctx context.Context, nzbPath string) (string, int64, error) {
	file, err := os.Open(nzbPath)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()

	nzb, err := nzbparser.Parse(file)
	if err != nil {
		return "", 0, err
	}

	if len(nzb.Files) == 0 {
		return "", 0, nil
	}

	// Extract filename from first file in NZB
	// Postie uses partial obfuscation, so filename should be preserved
	filename := filepath.Base(nzb.Files[0].Filename)

	// Calculate total size (excluding PAR2 files)
	var totalSize int64
	par2Pattern := strings.MustCompile(`(?i)\.par2$|\.p\d+$|\.vol\d+\+\d+\.par2$`)

	for _, file := range nzb.Files {
		if !par2Pattern.MatchString(file.Filename) {
			for _, segment := range file.Segments {
				totalSize += int64(segment.Bytes)
			}
		}
	}

	return filename, totalSize, nil
}

// filenamesMatch checks if two filenames are similar enough to be considered a match
// Handles common variations like underscores, dots, etc.
func (m *Matcher) filenamesMatch(original, incoming string) bool {
	// Normalize both filenames for comparison
	normalize := func(s string) string {
		s = strings.ToLower(s)
		// Replace common separators with space
		s = strings.ReplaceAll(s, "_", " ")
		s = strings.ReplaceAll(s, ".", " ")
		s = strings.ReplaceAll(s, "-", " ")
		// Collapse multiple spaces
		s = strings.Join(strings.Fields(s), " ")
		return s
	}

	origNorm := normalize(original)
	incomingNorm := normalize(incoming)

	// Direct match
	if origNorm == incomingNorm {
		return true
	}

	// Check if one contains the other (for cases where Postie might add/remove suffixes)
	if strings.Contains(origNorm, incomingNorm) || strings.Contains(incomingNorm, origNorm) {
		// But require at least 50% of the longer string to match
		longer := len(origNorm)
		if len(incomingNorm) > longer {
			longer = len(incomingNorm)
		}
		shorter := len(origNorm)
		if len(incomingNorm) < shorter {
			shorter = len(incomingNorm)
		}
		return float64(shorter)/float64(longer) >= 0.5
	}

	return false
}

// categoryMatches checks if the category matches based on file path
func (m *Matcher) categoryMatches(category string, nzbPath string) bool {
	nzbLower := strings.ToLower(nzbPath)
	categoryLower := strings.ToLower(category)

	return strings.Contains(nzbLower, categoryLower)
}

// CheckTimeouts checks for pending items that have exceeded the timeout
// Marks them as "postie_failed" so they can be retried manually
func (m *Matcher) CheckTimeouts(ctx context.Context) error {
	cfg := m.config
	timeout := time.Duration(cfg.TimeoutMinutes) * time.Minute

	pendingItems, err := m.queueRepo.GetPendingPostieItems(ctx)
	if err != nil {
		return err
	}

	now := time.Now()
	failedCount := 0

	for _, item := range pendingItems {
		// Check if item has exceeded timeout
		if now.Sub(item.UpdatedAt) > timeout {
			failedStatus := "postie_failed"
			m.log.InfoContext(ctx, "Postie upload timeout - marking as failed",
				"queue_id", item.ID,
				"original_name", item.OriginalReleaseName,
				"pending_since", item.UpdatedAt,
				"timeout_minutes", cfg.TimeoutMinutes)

			if err := m.queueRepo.UpdatePostieTracking(
				ctx,
				item.ID,
				item.PostieUploadID,
				&failedStatus,
				item.PostieUploadedAt,
				item.OriginalReleaseName,
			); err != nil {
				m.log.ErrorContext(ctx, "Failed to mark Postie timeout as failed",
					"queue_id", item.ID,
					"error", err)
			} else {
				failedCount++
			}
		}
	}

	if failedCount > 0 {
		m.log.InfoContext(ctx, "Marked Postie items as failed due to timeout", "count", failedCount)
	}

	return nil
}

// LinkPostieUpload links a Postie-generated NZB to the original queue item
// Updates the item's status to "uploading" and stores the upload ID
func (m *Matcher) LinkPostieUpload(ctx context.Context, itemID int64, nzbPath string) error {
	uploadingStatus := "postie_uploading"
	now := time.Now()

	// Generate a unique upload ID from the NZB path
	uploadID := filepath.Base(nzbPath)

	err := m.queueRepo.UpdatePostieTracking(
		ctx,
		itemID,
		&uploadID,
		&uploadingStatus,
		&now,
		nil, // Don't change original_release_name
	)

	if err != nil {
		return err
	}

	m.log.InfoContext(ctx, "Linked Postie upload to queue item",
		"queue_id", itemID,
		"upload_id", uploadID,
		"nzb_path", nzbPath)

	return nil
}

// CompletePostieUpload marks a Postie upload as completed
func (m *Matcher) CompletePostieUpload(ctx context.Context, itemID int64) error {
	completedStatus := "completed"
	now := time.Now()

	err := m.queueRepo.UpdatePostieTracking(
		ctx,
		itemID,
		nil, // Don't change upload_id
		&completedStatus,
		&now,
		nil, // Don't change original_release_name
	)

	if err != nil {
		return err
	}

	m.log.InfoContext(ctx, "Marked Postie upload as completed",
		"queue_id", itemID)

	return nil
}

// FailPostieUpload marks a Postie upload as failed
func (m *Matcher) FailPostieUpload(ctx context.Context, itemID int64, reason string) error {
	failedStatus := "postie_failed"

	err := m.queueRepo.UpdatePostieTracking(
		ctx,
		itemID,
		nil, // Don't change upload_id
		&failedStatus,
		nil, // Don't change uploaded_at
		nil, // Don't change original_release_name
	)

	if err != nil {
		return err
	}

	m.log.WarnContext(ctx, "Marked Postie upload as failed",
		"queue_id", itemID,
		"reason", reason)

	return nil
}

func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
