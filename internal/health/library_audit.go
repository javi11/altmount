package health

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/kipsilabs/altmount/internal/config"
	"github.com/kipsilabs/altmount/pkg/rclonecli"
)

// DefaultAuditGracePeriod keeps freshly imported files out of the orphan list:
// a file that has just finished importing has no library symlink until the
// ARR imports it, so it looks orphaned for a while.
const DefaultAuditGracePeriod = 24 * time.Hour

// auditMaxLinkHops bounds symlink chain resolution (library -> import dir ->
// mount), so a link loop cannot hang the scan.
const auditMaxLinkHops = 8

// importDirKeepDepth protects the import directory's own layout from empty
// folder pruning: the root, complete/ and complete/<category> stay even when
// empty; only release folders below them are removed.
const importDirKeepDepth = 2

// Where a dead symlink lives.
const (
	AuditLocationLibrary = "library"
	AuditLocationImport  = "import"
)

// auditOrphanWarnRatio is the orphan share above which the audit warns that
// the library may not be fully visible (an unmounted share, a wrong
// library_dir) before anyone deletes anything.
const auditOrphanWarnRatio = 0.2

// ErrLibrarySyncInProgress refuses a cleanup while library sync is running:
// both delete files, and sync's own view of the library is mid-change.
var ErrLibrarySyncInProgress = errors.New("library sync is running; try again when it finishes")

// LibraryAuditItem is one finding of a library audit.
type LibraryAuditItem struct {
	// Path is the mount-relative virtual path for an orphan, or the library
	// symlink path for a dead symlink.
	Path string `json:"path"`
	// Target is where a dead symlink points.
	Target string `json:"target,omitempty"`
	// Size is an orphan's file size in bytes.
	Size int64 `json:"size,omitempty"`
	// CreatedAt is when an orphan was imported (zero when unknown).
	CreatedAt time.Time `json:"created_at,omitzero"`
	// Location is where a dead symlink lives: the library, or AltMount's own
	// import directory (symlink import strategy).
	Location string `json:"location,omitempty"`
}

// LibraryAudit compares the library against AltMount's files: files in
// AltMount that no library symlink points at (orphans), and library symlinks
// into AltMount whose file no longer exists (dead symlinks).
type LibraryAudit struct {
	ScannedAt       time.Time `json:"scanned_at"`
	LibraryDir      string    `json:"library_dir"`
	MountPath       string    `json:"mount_path"`
	ImportDir       string    `json:"import_dir,omitempty"`
	FilesScanned    int       `json:"files_scanned"`
	SymlinksScanned int       `json:"symlinks_scanned"`
	// ImportSymlinksScanned counts links in the import directory. They never
	// make a file "referenced": the library is what the media server reads.
	ImportSymlinksScanned int                `json:"import_symlinks_scanned"`
	Orphans               []LibraryAuditItem `json:"orphans"`
	DeadSymlinks          []LibraryAuditItem `json:"dead_symlinks"`
	// SkippedRecent counts unreferenced files left out of Orphans because they
	// were imported within the grace period.
	SkippedRecent      int     `json:"skipped_recent"`
	GracePeriodSeconds float64 `json:"grace_period_seconds"`
	WalkErrors         int     `json:"walk_errors"`
	// OrphanCleanupBlocked explains why orphans may not be deleted (the
	// library looks unreadable); empty when cleanup is allowed.
	OrphanCleanupBlocked string `json:"orphan_cleanup_blocked,omitempty"`
	// Warning flags a result worth a second look before cleaning up.
	Warning string `json:"warning,omitempty"`
}

// LibraryAuditCleanupResult reports what a cleanup removed.
type LibraryAuditCleanupResult struct {
	OrphansDeleted      int                `json:"orphans_deleted"`
	DeadSymlinksRemoved int                `json:"dead_symlinks_removed"`
	Skipped             []LibraryAuditSkip `json:"skipped"`
	Audit               *LibraryAudit      `json:"audit"`
}

// LibraryAuditSkip is a requested item the cleanup left alone, and why.
type LibraryAuditSkip struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// AuditLibrary scans the library and AltMount's metadata and reports orphans
// and dead symlinks without changing anything.
func (lsw *LibrarySyncWorker) AuditLibrary(ctx context.Context, grace time.Duration) (*LibraryAudit, error) {
	cfg := lsw.configGetter()
	if cfg.Health.LibraryDir == nil || strings.TrimSpace(*cfg.Health.LibraryDir) == "" {
		return nil, errors.New("library directory is not configured (Health → Library Directory)")
	}
	if strings.TrimSpace(cfg.MountPath) == "" {
		return nil, errors.New("mount path is not configured")
	}
	libraryDir := filepath.Clean(*cfg.Health.LibraryDir)
	mountDir := filepath.Clean(cfg.MountPath)
	importDir := ""
	if cfg.Import.ImportDir != nil && *cfg.Import.ImportDir != "" {
		importDir = filepath.Clean(*cfg.Import.ImportDir)
	}

	audit := &LibraryAudit{
		ScannedAt:          time.Now().UTC(),
		LibraryDir:         libraryDir,
		MountPath:          mountDir,
		ImportDir:          importDir,
		Orphans:            []LibraryAuditItem{},
		DeadSymlinks:       []LibraryAuditItem{},
		GracePeriodSeconds: grace.Seconds(),
	}

	metaFiles, err := lsw.getAllMetadataFiles(ctx)
	if err != nil {
		return nil, fmt.Errorf("scan metadata: %w", err)
	}
	files := make(map[string]bool, len(metaFiles))
	for _, metaPath := range metaFiles {
		rel := lsw.metaPathToMountRelativePath(metaPath)
		if rel == "" || strings.HasPrefix(rel, ".") || strings.Contains(rel, "/.") {
			continue // hidden bookkeeping (.ids and the like), never a library file
		}
		files[rel] = true
	}
	audit.FilesScanned = len(files)

	referenced := make(map[string]bool, len(files))
	// A symlink to a whole directory (a season folder, say) references every
	// file under it.
	referencedDirs := map[string]bool{}
	walkErr := filepath.WalkDir(libraryDir, func(p string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			audit.WalkErrors++
			return nil
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, finalTarget, dead, ours := resolveAuditLink(p, mountDir, importDir)
		if !ours {
			return nil
		}
		audit.SymlinksScanned++
		if rel != "" {
			switch {
			case files[rel]:
				referenced[rel] = true
				return nil
			case lsw.metadataService.DirectoryExists(rel):
				referencedDirs[rel] = true
				return nil
			}
			dead = true
		}
		if dead {
			audit.DeadSymlinks = append(audit.DeadSymlinks, LibraryAuditItem{Path: p, Target: finalTarget, Location: AuditLocationLibrary})
		}
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("scan library %s: %w", libraryDir, walkErr)
	}

	// The import directory holds AltMount's own links for the ARRs to pick
	// up. Nothing else removes them, so links whose file was later deleted
	// pile up there.
	if importDir != "" && !within(importDir, libraryDir) && importDir != libraryDir {
		if err := lsw.auditImportDir(ctx, audit, importDir, mountDir, files); err != nil {
			return nil, err
		}
	}

	cutoff := time.Now().Add(-grace)
	for rel := range files {
		if referenced[rel] || underReferencedDir(rel, referencedDirs) {
			continue
		}
		item := LibraryAuditItem{Path: rel}
		if lite, err := lsw.metadataService.ReadFileMetadataLite(rel); err == nil && lite != nil {
			item.Size = lite.FileSize
		}
		if fm, err := lsw.metadataService.ReadFileMetadata(rel); err == nil && fm != nil && fm.GetCreatedAt() > 0 {
			item.CreatedAt = time.Unix(fm.GetCreatedAt(), 0).UTC()
		}
		if grace > 0 && !item.CreatedAt.IsZero() && item.CreatedAt.After(cutoff) {
			audit.SkippedRecent++
			continue
		}
		audit.Orphans = append(audit.Orphans, item)
	}
	sort.Slice(audit.Orphans, func(i, j int) bool { return audit.Orphans[i].Path < audit.Orphans[j].Path })
	sort.Slice(audit.DeadSymlinks, func(i, j int) bool { return audit.DeadSymlinks[i].Path < audit.DeadSymlinks[j].Path })

	switch {
	case audit.WalkErrors > 0:
		audit.OrphanCleanupBlocked = fmt.Sprintf("%d parts of the library could not be read; orphan cleanup is disabled until a clean scan", audit.WalkErrors)
	case audit.SymlinksScanned == 0 && audit.FilesScanned > 0:
		audit.OrphanCleanupBlocked = "no library symlinks into AltMount were found; the library may not be mounted, so orphan cleanup is disabled"
	}
	if audit.FilesScanned > 0 && float64(len(audit.Orphans)) > auditOrphanWarnRatio*float64(audit.FilesScanned) {
		audit.Warning = fmt.Sprintf("%d of %d files (%.0f%%) look orphaned — check the library directory is correct and fully mounted before cleaning up",
			len(audit.Orphans), audit.FilesScanned, 100*float64(len(audit.Orphans))/float64(audit.FilesScanned))
	}
	return audit, nil
}

// auditImportDir adds the import directory's dead links to the audit.
func (lsw *LibrarySyncWorker) auditImportDir(ctx context.Context, audit *LibraryAudit, importDir, mountDir string, files map[string]bool) error {
	if _, err := os.Stat(importDir); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		audit.WalkErrors++
		return nil
	}
	err := filepath.WalkDir(importDir, func(p string, d os.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			audit.WalkErrors++
			return nil
		}
		if d.Type()&os.ModeSymlink == 0 {
			return nil
		}
		rel, finalTarget, dead, ours := resolveAuditLink(p, mountDir, importDir)
		if !ours && !dead {
			return nil
		}
		audit.ImportSymlinksScanned++
		if rel != "" && (files[rel] || lsw.metadataService.DirectoryExists(rel)) {
			return nil
		}
		audit.DeadSymlinks = append(audit.DeadSymlinks, LibraryAuditItem{Path: p, Target: finalTarget, Location: AuditLocationImport})
		return nil
	})
	if err != nil {
		return fmt.Errorf("scan import directory %s: %w", importDir, err)
	}
	return nil
}

// CleanupLibraryAudit deletes the requested orphans and dead symlinks. It runs
// a fresh audit first and acts only on items that are still orphaned or dead
// now, so a file the ARR linked (or a symlink it repaired) since the dry run is
// never touched.
func (lsw *LibrarySyncWorker) CleanupLibraryAudit(ctx context.Context, orphans, deadSymlinks []string, grace time.Duration) (*LibraryAuditCleanupResult, error) {
	if lsw.GetStatus().IsRunning {
		return nil, ErrLibrarySyncInProgress
	}
	audit, err := lsw.AuditLibrary(ctx, grace)
	if err != nil {
		return nil, err
	}
	result := &LibraryAuditCleanupResult{Skipped: []LibraryAuditSkip{}}

	currentOrphans := make(map[string]bool, len(audit.Orphans))
	for _, o := range audit.Orphans {
		currentOrphans[o.Path] = true
	}
	currentDead := make(map[string]bool, len(audit.DeadSymlinks))
	for _, d := range audit.DeadSymlinks {
		currentDead[d.Path] = true
	}

	deletedDirs := map[string]bool{}
	for _, rel := range orphans {
		switch {
		case audit.OrphanCleanupBlocked != "":
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: rel, Reason: audit.OrphanCleanupBlocked})
			continue
		case !currentOrphans[rel]:
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: rel, Reason: "no longer orphaned"})
			continue
		}
		if err := lsw.metadataService.DeleteFileMetadata(ctx, rel); err != nil && !os.IsNotExist(err) {
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: rel, Reason: err.Error()})
			continue
		}
		if lsw.healthRepo != nil {
			// Most orphans have a health record; one without is not an error.
			_ = lsw.healthRepo.DeleteHealthRecord(ctx, rel)
		}
		deletedDirs[path.Dir(rclonecli.ToVFSPath(rel))] = true
		result.OrphansDeleted++
		slog.InfoContext(ctx, "Library audit deleted orphaned file", "path", rel)
	}

	for _, link := range deadSymlinks {
		clean := filepath.Clean(link)
		inImport := within(clean, audit.ImportDir)
		switch {
		case !currentDead[clean]:
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: link, Reason: "no longer a dead symlink"})
			continue
		case !within(clean, audit.LibraryDir) && !inImport:
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: link, Reason: "outside the library and import directories"})
			continue
		}
		info, err := os.Lstat(clean)
		if err != nil || info.Mode()&os.ModeSymlink == 0 {
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: link, Reason: "not a symlink"})
			continue
		}
		if err := os.Remove(clean); err != nil {
			result.Skipped = append(result.Skipped, LibraryAuditSkip{Path: link, Reason: err.Error()})
			continue
		}
		result.DeadSymlinksRemoved++
		slog.InfoContext(ctx, "Library audit removed dead symlink", "path", clean)
		if inImport {
			pruneEmptyImportDirs(filepath.Dir(clean), audit.ImportDir)
		}
	}

	lsw.refreshRcloneDirs(deletedDirs)

	// Report the library as it stands after the cleanup.
	if after, err := lsw.AuditLibrary(ctx, grace); err == nil {
		result.Audit = after
	} else {
		result.Audit = audit
	}
	return result, nil
}

// refreshRcloneDirs tells an rclone mount to drop its cached listings of the
// given virtual directories after files in them were deleted.
func (lsw *LibrarySyncWorker) refreshRcloneDirs(dirs map[string]bool) {
	if lsw.rcloneClient == nil || len(dirs) == 0 {
		return
	}
	cfg := lsw.configGetter()
	switch cfg.MountType {
	case config.MountTypeRClone, config.MountTypeRCloneExternal:
	default:
		return
	}
	list := make([]string, 0, len(dirs))
	for d := range dirs {
		list = append(list, d)
	}
	vfsName := cfg.RClone.VFSName
	if vfsName == "" {
		vfsName = config.MountProvider
	}
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		if err := lsw.rcloneClient.RefreshDir(c, vfsName, list); err != nil {
			slog.ErrorContext(c, "Failed to notify rclone VFS to refresh directories", "vfs", vfsName, "dir_count", len(list), "err", err)
		}
	}()
}

// resolveAuditLink follows a library symlink by reading link targets only —
// never stat'ing through the mount — through any chain of symlinks (for
// example library -> import dir -> mount).
//
// ours reports whether the link belongs to AltMount: it reaches the mount, or
// dies inside the import directory. rel is the mount-relative path it reaches
// (empty when it dies before the mount); dead reports a broken chain; final
// is the last target followed.
func resolveAuditLink(link, mountDir, importDir string) (rel, final string, dead, ours bool) {
	current := link
	for hop := 0; hop < auditMaxLinkHops; hop++ {
		target, err := os.Readlink(current)
		if err != nil {
			return "", current, true, final != "" && within(current, importDir)
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(current), target)
		}
		target = filepath.Clean(target)
		final = target
		if within(target, mountDir) {
			r, err := filepath.Rel(mountDir, target)
			if err != nil {
				return "", target, false, false
			}
			return filepath.ToSlash(r), target, false, true
		}
		info, err := os.Lstat(target)
		switch {
		case err != nil:
			// Missing link target: ours only if it was meant to land in the
			// import directory, which only holds links into the mount.
			return "", target, true, within(target, importDir)
		case info.Mode()&os.ModeSymlink != 0:
			current = target
		default:
			return "", target, false, false // a real file outside AltMount
		}
	}
	return "", final, true, within(final, mountDir) || within(final, importDir)
}

// pruneEmptyImportDirs removes dir and its now-empty parents inside the
// import directory, stopping at the layout it must keep (see
// importDirKeepDepth). os.Remove refuses non-empty directories, so a folder
// with anything left in it always survives.
func pruneEmptyImportDirs(dir, importDir string) {
	for within(dir, importDir) {
		rel, err := filepath.Rel(importDir, dir)
		if err != nil || len(strings.Split(rel, string(filepath.Separator))) <= importDirKeepDepth {
			return
		}
		if err := os.Remove(dir); err != nil {
			return
		}
		dir = filepath.Dir(dir)
	}
}

// underReferencedDir reports whether a mount-relative file sits inside a
// directory that a library symlink points at.
func underReferencedDir(rel string, dirs map[string]bool) bool {
	if len(dirs) == 0 {
		return false
	}
	for dir := path.Dir(rel); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if dirs[dir] {
			return true
		}
	}
	return false
}

// within reports whether p is strictly inside dir (not dir itself, and not a
// sibling that merely shares the prefix, like /mnt/altmount_symlinks for
// /mnt/altmount).
func within(p, dir string) bool {
	if dir == "" {
		return false
	}
	return strings.HasPrefix(p, dir+string(filepath.Separator))
}
