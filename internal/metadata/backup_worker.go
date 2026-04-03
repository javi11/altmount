package metadata

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/javi11/altmount/internal/config"
	"github.com/robfig/cron/v3"
)

type BackupWorker struct {
	configGetter  config.ConfigGetter
	cronRunner    *cron.Cron
	workerCtx     context.Context
	workerCancel  context.CancelFunc
	workerMu      sync.Mutex
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

	w.cronRunner = cron.New(cron.WithLocation(time.UTC))
	if _, err := w.cronRunner.AddFunc(cfg.Metadata.Backup.Schedule, w.performBackup); err != nil {
		w.workerCancel()
		return fmt.Errorf("invalid backup schedule %q: %w", cfg.Metadata.Backup.Schedule, err)
	}
	w.cronRunner.Start()
	w.workerRunning = true

	slog.InfoContext(ctx, "Metadata backup worker started",
		"schedule", cfg.Metadata.Backup.Schedule,
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
	cronCtx := w.cronRunner.Stop()
	<-cronCtx.Done()
	w.cronRunner = nil
	w.workerRunning = false
	slog.InfoContext(ctx, "Metadata backup worker stopped")
}

func (w *BackupWorker) performBackup() {
	cfg := w.configGetter()
	backupRoot := cfg.Metadata.Backup.Path
	metadataDir := cfg.Metadata.RootPath

	timestamp := time.Now().Format("20060102-150405")
	backupDir := filepath.Join(backupRoot, timestamp)

	if err := os.MkdirAll(backupDir, 0755); err != nil {
		slog.ErrorContext(w.workerCtx, "Failed to create backup directory", "error", err, "path", backupDir)
		return
	}

	slog.InfoContext(w.workerCtx, "Starting metadata backup (copy)", "destination", backupDir)

	count := 0
	err := filepath.Walk(metadataDir, func(path string, info os.FileInfo, err error) error {
		if w.workerCtx != nil {
			select {
			case <-w.workerCtx.Done():
				return w.workerCtx.Err()
			default:
			}
		}

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
		if errors.Is(err, context.Canceled) {
			slog.InfoContext(w.workerCtx, "Metadata backup canceled")
		} else {
			slog.ErrorContext(w.workerCtx, "Failed to complete metadata backup", "error", err)
		}
		// Cleanup failed partial backup
		os.RemoveAll(backupDir)
		return
	}

	slog.InfoContext(w.workerCtx, "Metadata backup completed successfully", "files_copied", count)

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
		slog.ErrorContext(w.workerCtx, "Failed to read backup directory for cleanup", "error", err)
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
		slog.InfoContext(w.workerCtx, "Deleting old backup directory", "path", path)
		if err := os.RemoveAll(path); err != nil {
			slog.ErrorContext(w.workerCtx, "Failed to delete old backup directory", "error", err, "path", path)
		}
	}
}
