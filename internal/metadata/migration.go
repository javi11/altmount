package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
)

// defaultMigrationGroup is used when the config leaves default_group empty.
// Without a group, nzb.BuildNZB emits an empty <groups> element and most NZB
// clients reject the result.
const defaultMigrationGroup = "alt.binaries.misc"

// migrationGroupPause paces the worker so a large library does not saturate
// disk while streams are being served.
const migrationGroupPause = 100 * time.Millisecond

// ErrMigrationRunning is returned when a second run is requested.
var ErrMigrationRunning = errors.New("metadata migration already running")

// MigrationProgress is the live progress of a run.
type MigrationProgress struct {
	TotalGroups     int       `json:"total_groups"`
	ProcessedGroups int       `json:"processed_groups"`
	TotalFiles      int       `json:"total_files"`
	ProcessedFiles  int       `json:"processed_files"`
	CurrentRelease  string    `json:"current_release"`
	StartTime       time.Time `json:"start_time"`
}

// MigrationResult summarises a completed run (real or dry).
type MigrationResult struct {
	DryRun            bool          `json:"dry_run"`
	Groups            int           `json:"groups"`
	FaithfulGroups    int           `json:"faithful_groups"`
	SynthesizedGroups int           `json:"synthesized_groups"`
	FilesMigrated     int           `json:"files_migrated"`
	FilesFailed       int           `json:"files_failed"`
	BytesBefore       int64         `json:"bytes_before"`
	BytesAfter        int64         `json:"bytes_after"`
	BytesSaved        int64         `json:"bytes_saved"`
	Failures          []string      `json:"failures,omitempty"`
	Cancelled         bool          `json:"cancelled"`
	Duration          time.Duration `json:"duration"`
	CompletedAt       time.Time     `json:"completed_at"`
}

// MigrationStatus is what the API reports.
type MigrationStatus struct {
	IsRunning    bool               `json:"is_running"`
	LegacyFiles  int                `json:"legacy_files"`
	LegacyGroups int                `json:"legacy_groups"`
	Progress     *MigrationProgress `json:"progress,omitempty"`
	LastResult   *MigrationResult   `json:"last_result,omitempty"`
	LastDryRun   *MigrationResult   `json:"last_dry_run,omitempty"`
}

// MigrationWorker converts legacy inline-segment metadata to the v3
// store-backed format. It is manually triggered; nothing runs on startup.
type MigrationWorker struct {
	ms           *MetadataService
	configGetter config.ConfigGetter

	mu         sync.Mutex
	running    bool
	cancelFunc context.CancelFunc

	progressMu sync.RWMutex
	progress   *MigrationProgress
	lastResult *MigrationResult
	lastDryRun *MigrationResult
}

// NewMigrationWorker creates a migration worker. It does not start anything.
func NewMigrationWorker(ms *MetadataService, configGetter config.ConfigGetter) *MigrationWorker {
	return &MigrationWorker{ms: ms, configGetter: configGetter}
}

// GetStatus reports whether a run is active, its progress, and the last results.
// The legacy counts come from the live progress when running and are otherwise
// left at zero; callers wanting a fresh count run a dry run.
func (w *MigrationWorker) GetStatus() MigrationStatus {
	w.mu.Lock()
	running := w.running
	w.mu.Unlock()

	w.progressMu.RLock()
	defer w.progressMu.RUnlock()

	status := MigrationStatus{
		IsRunning:  running,
		LastResult: w.lastResult,
		LastDryRun: w.lastDryRun,
	}
	if w.progress != nil {
		p := *w.progress
		status.Progress = &p
		status.LegacyFiles = p.TotalFiles
		status.LegacyGroups = p.TotalGroups
	}
	return status
}

// Cancel stops an in-flight run after the current file.
func (w *MigrationWorker) Cancel() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cancelFunc != nil {
		w.cancelFunc()
	}
}

// Start launches a migration in the background and returns immediately.
// The run is detached from the caller's context so cancelling an HTTP request
// does not abort a migration; use Cancel for that.
func (w *MigrationWorker) Start(ctx context.Context) error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return ErrMigrationRunning
	}
	runCtx, cancel := context.WithCancel(context.Background())
	w.running = true
	w.cancelFunc = cancel
	w.mu.Unlock()

	slog.InfoContext(ctx, "Starting metadata migration to v3 store format")

	go func() {
		defer func() {
			w.mu.Lock()
			w.running = false
			w.cancelFunc = nil
			w.mu.Unlock()
			cancel()
		}()

		result, err := w.run(runCtx, false)
		if err != nil {
			slog.ErrorContext(runCtx, "Metadata migration failed", "error", err)
			return
		}
		w.progressMu.Lock()
		w.lastResult = result
		w.progressMu.Unlock()
		slog.InfoContext(runCtx, "Metadata migration finished",
			"files_migrated", result.FilesMigrated,
			"files_failed", result.FilesFailed,
			"bytes_saved", result.BytesSaved,
			"cancelled", result.Cancelled)
	}()
	return nil
}

// DryRun performs the real conversion against an isolated temporary metadata
// root, measures the result, and deletes it. Nothing in the library is touched
// and no store reference counts move (the temporary service has no counter).
func (w *MigrationWorker) DryRun(ctx context.Context) (*MigrationResult, error) {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return nil, ErrMigrationRunning
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.running = true
	w.cancelFunc = cancel
	w.mu.Unlock()

	defer func() {
		w.mu.Lock()
		w.running = false
		w.cancelFunc = nil
		w.mu.Unlock()
		cancel()
	}()

	result, err := w.run(runCtx, true)
	if err != nil {
		return nil, err
	}
	w.progressMu.Lock()
	w.lastDryRun = result
	w.progressMu.Unlock()
	return result, nil
}

// run is the shared body of Start and DryRun.
func (w *MigrationWorker) run(ctx context.Context, dryRun bool) (*MigrationResult, error) {
	started := time.Now()

	groups, err := w.ms.ScanLegacyMetas()
	if err != nil {
		return nil, fmt.Errorf("scan legacy metadata: %w", err)
	}

	totalFiles := 0
	for _, g := range groups {
		totalFiles += len(g.Files)
	}
	w.setProgress(&MigrationProgress{
		TotalGroups: len(groups),
		TotalFiles:  totalFiles,
		StartTime:   started,
	})

	target := w.ms
	storeDir := w.storeDir()
	if dryRun {
		tmpRoot, tmpErr := os.MkdirTemp("", "altmount-migration-dryrun-")
		if tmpErr != nil {
			return nil, fmt.Errorf("create dry-run temp root: %w", tmpErr)
		}
		defer func() { _ = os.RemoveAll(tmpRoot) }()
		target = NewMetadataService(filepath.Join(tmpRoot, "meta"))
		storeDir = filepath.Join(tmpRoot, "store")
	}

	result := &MigrationResult{DryRun: dryRun, Groups: len(groups)}
	defaultGroup := w.defaultGroup()

	for _, g := range groups {
		if ctx.Err() != nil {
			result.Cancelled = true
			break
		}
		w.updateProgressGroup(g.Key)

		// A dry run converts into a throwaway root, so it must hydrate the
		// group from the real service: the temporary one has no .meta files.
		hydrated := g
		if dryRun {
			loaded, loadErr := w.ms.LoadGroupMetas(g)
			if loadErr != nil {
				result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", g.Key, loadErr))
				continue
			}
			hydrated = loaded
		}

		gr, groupErr := target.MigrateGroup(ctx, hydrated, storeDir, defaultGroup)
		if groupErr != nil {
			if ctx.Err() != nil {
				result.Cancelled = true
				break
			}
			result.FilesFailed += len(g.Files)
			result.Failures = append(result.Failures, fmt.Sprintf("%s: %v", g.Key, groupErr))
			slog.WarnContext(ctx, "Skipping release group that failed to migrate",
				"group", g.Key, "error", groupErr)
			continue
		}

		if gr.Faithful {
			result.FaithfulGroups++
		} else {
			result.SynthesizedGroups++
		}
		result.FilesMigrated += gr.FilesMigrated
		result.FilesFailed += gr.FilesFailed
		result.BytesBefore += gr.BytesBefore
		result.BytesAfter += gr.BytesAfter
		result.Failures = append(result.Failures, gr.Failures...)

		w.advanceProgress(len(g.Files))
		if !dryRun {
			select {
			case <-ctx.Done():
				result.Cancelled = true
			case <-time.After(migrationGroupPause):
			}
		}
	}

	result.BytesSaved = result.BytesBefore - result.BytesAfter
	result.Duration = time.Since(started)
	result.CompletedAt = time.Now()
	w.setProgress(nil)
	return result, nil
}

// storeDir is where migrated .nzbz files live: <configDir>/.nzbs/_migrated,
// mirroring the importer's layout.
func (w *MigrationWorker) storeDir() string {
	cfg := w.configGetter()
	configDir := filepath.Dir(cfg.Database.Path)
	if !filepath.IsAbs(configDir) {
		if abs, err := filepath.Abs(configDir); err == nil {
			configDir = abs
		}
	}
	return filepath.Join(configDir, ".nzbs", "_migrated")
}

func (w *MigrationWorker) defaultGroup() string {
	if g := w.configGetter().Metadata.Migration.DefaultGroup; g != "" {
		return g
	}
	return defaultMigrationGroup
}

func (w *MigrationWorker) setProgress(p *MigrationProgress) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	w.progress = p
}

func (w *MigrationWorker) updateProgressGroup(key string) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	if w.progress != nil {
		w.progress.CurrentRelease = key
	}
}

func (w *MigrationWorker) advanceProgress(files int) {
	w.progressMu.Lock()
	defer w.progressMu.Unlock()
	if w.progress != nil {
		w.progress.ProcessedGroups++
		w.progress.ProcessedFiles += files
	}
}
