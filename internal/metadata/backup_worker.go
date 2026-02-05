package metadata

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
)

type BackupWorker struct {
	configGetter config.ConfigGetter
	workerCtx    context.Context
	workerCancel context.CancelFunc
	workerWg     sync.WaitGroup
	workerMu     sync.Mutex
	workerRunning bool
}

func NewBackupWorker(configGetter config.ConfigGetter) *BackupWorker {
	return &BackupWorker{
		configGetter: configGetter,
	}
}

func (w *BackupWorker) Start(ctx context.Context) error {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	if w.workerRunning {
		return nil
	}

	cfg := w.configGetter()
	if cfg.Metadata.Backup.Enabled == nil || !*cfg.Metadata.Backup.Enabled {
		return nil
	}

	w.workerCtx, w.workerCancel = context.WithCancel(ctx)
	w.workerRunning = true

	w.workerWg.Add(1)
	go w.runWorker()

	slog.InfoContext(ctx, "Metadata backup worker started",
		"interval_hours", cfg.Metadata.Backup.IntervalHours,
		"keep_backups", cfg.Metadata.Backup.KeepBackups,
		"path", cfg.Metadata.Backup.Path)
	return nil
}

func (w *BackupWorker) Stop(ctx context.Context) {
	w.workerMu.Lock()
	defer w.workerMu.Unlock()

	if !w.workerRunning {
		return
	}

	w.workerCancel()
	w.workerWg.Wait()
	w.workerRunning = false
	slog.InfoContext(ctx, "Metadata backup worker stopped")
}

func (w *BackupWorker) runWorker() {
	defer w.workerWg.Done()

	ticker := time.NewTicker(w.configGetter().GetMetadataBackupInterval())
	defer ticker.Stop()

	// Initial backup after a short delay
	select {
	case <-time.After(1 * time.Minute):
		w.performBackup()
	case <-w.workerCtx.Done():
		return
	}

	for {
		select {
		case <-ticker.C:
			w.performBackup()
		case <-w.workerCtx.Done():
			return
		}
	}
}

func (w *BackupWorker) performBackup() {
	cfg := w.configGetter()
	backupRoot := cfg.Metadata.Backup.Path
	metadataDir := cfg.Metadata.RootPath

	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(backupRoot, timestamp)

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		slog.Error("Failed to create backup directory", "error", err, "path", backupDir)
		return
	}

	slog.Info("Starting metadata backup (copy)", "destination", backupDir)

	count := 0
	err := filepath.Walk(metadataDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		if !strings.HasSuffix(info.Name(), ".meta") {
			return nil
		}

		relPath, err := filepath.Rel(metadataDir, path)
		if err != nil {
			return err
		}

		destPath := filepath.Join(backupDir, relPath)
		if err := os.MkdirAll(filepath.Dir(destPath), 0755); err != nil {
			return err
		}

		if err := w.copyFile(path, destPath); err != nil {
			return err
		}
		count++
		return nil
	})

	if err != nil {
		slog.Error("Failed to complete metadata backup", "error", err)
		// Cleanup failed partial backup
		os.RemoveAll(backupDir)
		return
	}

	slog.Info("Metadata backup completed successfully", "files_copied", count)

	w.cleanupOldBackups(backupRoot, cfg.GetMetadataBackupKeep())
}

func (w *BackupWorker) copyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sourceFile.Close()

	destFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer destFile.Close()

	_, err = io.Copy(destFile, sourceFile)
	return err
}

func (w *BackupWorker) cleanupOldBackups(backupRoot string, keep int) {
	files, err := os.ReadDir(backupRoot)
	if err != nil {
		slog.Error("Failed to read backup directory for cleanup", "error", err)
		return
	}

	type backupEntry struct {
		name    string
		modTime time.Time
	}

	var backups []backupEntry
	for _, f := range files {
		if f.IsDir() {
			info, err := f.Info()
			if err == nil {
				backups = append(backups, backupEntry{
					name:    f.Name(),
					modTime: info.ModTime(),
				})
			}
		}
	}

	if len(backups) <= keep {
		return
	}

	sort.Slice(backups, func(i, j int) bool {
		return backups[i].modTime.After(backups[j].modTime)
	})

	for i := keep; i < len(backups); i++ {
		path := filepath.Join(backupRoot, backups[i].name)
		slog.Info("Deleting old backup directory", "path", path)
		if err := os.RemoveAll(path); err != nil {
			slog.Error("Failed to delete old backup directory", "error", err, "path", path)
		}
	}
}