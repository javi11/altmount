package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/javi11/altmount/internal/arrs/model"
	"github.com/javi11/altmount/internal/arrs/registrar"
	"github.com/javi11/altmount/internal/config"
	"golift.io/starr"
	"golift.io/starr/sonarr"
)

// StuckCleanupResult summarizes a stuck-import cleanup run across all instances.
type StuckCleanupResult struct {
	Instances    []InstanceCleanupResult `json:"instances"`
	TotalBlocked int                     `json:"total_blocked"`
}

// InstanceCleanupResult is the per-instance outcome of a stuck-import cleanup run.
type InstanceCleanupResult struct {
	Instance string `json:"instance"`
	Type     string `json:"type"`
	Blocked  int    `json:"blocked"`
	Error    string `json:"error,omitempty"`
}

// stuckItem is a normalized view of an *arr queue record across all client types,
// holding only the fields the stuck-import detection needs.
type stuckItem struct {
	ID                    int64
	Title                 string
	TrackedDownloadStatus string
	TrackedDownloadState  string // empty when the *arr does not report it (e.g. Lidarr)
	DownloadClient        string
	Messages              []string
}

// IsStuckCleanupEnabled reports whether the background stuck-import cleanup pass
// should run, based on arrs.enabled and arrs.stuck_cleanup_enabled.
func IsStuckCleanupEnabled(cfg *config.Config) bool {
	if cfg.Arrs.Enabled == nil || !*cfg.Arrs.Enabled {
		return false
	}
	return cfg.Arrs.StuckCleanupEnabled != nil && *cfg.Arrs.StuckCleanupEnabled
}

// CleanupStuckQueue scans every enabled *arr instance for items AltMount sent that
// are stuck importing for a known reason, then removes and blocklists them so the
// release is not grabbed again and the *arr searches for a replacement.
//
// When force is false an item is only acted on after it has been continuously
// observed stuck for the configured grace period (transient errors that the *arr
// resolves on its own are left alone). When force is true the grace period is
// bypassed and everything currently matching is blocklisted immediately.
//
// The automatic periodic run is gated by IsStuckCleanupEnabled at the caller
// (the worker tick); this method itself only requires arrs to be enabled, so the
// manual trigger works regardless of the periodic toggle.
func (w *Worker) CleanupStuckQueue(ctx context.Context, force bool) (*StuckCleanupResult, error) {
	cfg := w.configGetter()
	result := &StuckCleanupResult{Instances: []InstanceCleanupResult{}}

	if cfg.Arrs.Enabled == nil || !*cfg.Arrs.Enabled {
		return result, nil
	}

	for _, instance := range w.instances.GetAllInstances() {
		if instance == nil || !instance.Enabled {
			continue
		}

		var blocked int
		var err error
		switch instance.Type {
		case "radarr":
			blocked, err = w.cleanupStuckRadarr(ctx, instance, cfg, force)
		case "sonarr":
			blocked, err = w.cleanupStuckSonarr(ctx, instance, cfg, force, false)
		case "whisparr":
			blocked, err = w.cleanupStuckSonarr(ctx, instance, cfg, force, true)
		case "lidarr":
			blocked, err = w.cleanupStuckLidarr(ctx, instance, cfg, force)
		case "readarr":
			blocked, err = w.cleanupStuckReadarr(ctx, instance, cfg, force)
		case "sportarr":
			blocked, err = w.cleanupStuckSportarr(ctx, instance, cfg, force)
		default:
			continue
		}

		res := InstanceCleanupResult{Instance: instance.Name, Type: instance.Type, Blocked: blocked}
		if err != nil {
			res.Error = err.Error()
			slog.WarnContext(ctx, "Failed to clean up stuck imports",
				"instance", instance.Name, "type", instance.Type, "error", err)
		}
		result.Instances = append(result.Instances, res)
		result.TotalBlocked += blocked
	}

	if result.TotalBlocked > 0 {
		slog.InfoContext(ctx, "Stuck import cleanup acted on releases",
			"count", result.TotalBlocked, "force", force)
	}
	return result, nil
}

// stuckAction is a queue item selected for cleanup plus how to act on it
// (one of the config.StuckAction* values).
type stuckAction struct {
	ID     int64
	Action string
}

// matchStuckRule returns the first enabled rule whose message matches any of the
// item's status messages (case-insensitive substring), or nil when none match.
func matchStuckRule(messages []string, rules []config.StuckCleanupRule) *config.StuckCleanupRule {
	if len(messages) == 0 {
		return nil
	}
	joined := strings.ToLower(strings.Join(messages, " "))
	for i := range rules {
		r := &rules[i]
		if !r.Enabled || r.Message == "" {
			continue
		}
		if strings.Contains(joined, strings.ToLower(r.Message)) {
			return r
		}
	}
	return nil
}

// stuckRuleFor returns the cleanup rule for an item, or nil if it is not stuck.
// An item must be flagged with a warning by the *arr and match an enabled rule.
func stuckRuleFor(item stuckItem, cfg *config.Config) *config.StuckCleanupRule {
	if !strings.EqualFold(item.TrackedDownloadStatus, "warning") {
		return nil
	}
	return matchStuckRule(item.Messages, cfg.Arrs.StuckCleanupRules)
}

// selectStuckActions filters AltMount-owned queue items to those that should be
// cleaned now, carrying each item's blocklist decision. With force, all matching
// items are returned immediately. Otherwise an item must have been observed stuck
// for the configured grace period; first observations and items the *arr has since
// resolved are tracked/cleared via the shared firstSeen map.
func (w *Worker) selectStuckActions(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, items []stuckItem, force bool) []stuckAction {
	var actions []stuckAction
	gracePeriod := time.Duration(cfg.Arrs.StuckCleanupGracePeriodMinutes) * time.Minute

	for _, item := range items {
		// Only ever touch items owned by AltMount's download client — other
		// clients may reference paths AltMount cannot see (see issue #523).
		if item.DownloadClient != registrar.AltmountDownloadClientName {
			continue
		}

		key := fmt.Sprintf("stuck|%s|%d", instance.Name, item.ID)

		rule := stuckRuleFor(item, cfg)
		if rule == nil {
			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
			continue
		}

		if force || gracePeriod <= 0 {
			actions = append(actions, stuckAction{ID: item.ID, Action: rule.Action})
			w.firstSeenMu.Lock()
			delete(w.firstSeen, key)
			w.firstSeenMu.Unlock()
			continue
		}

		w.firstSeenMu.Lock()
		seenTime, exists := w.firstSeen[key]
		if !exists {
			w.firstSeen[key] = time.Now()
			w.firstSeenMu.Unlock()
			slog.DebugContext(ctx, "First saw stuck import, starting grace period",
				"title", item.Title, "instance", instance.Name)
			continue
		}
		w.firstSeenMu.Unlock()

		if time.Since(seenTime) < gracePeriod {
			continue
		}

		actions = append(actions, stuckAction{ID: item.ID, Action: rule.Action})
		w.firstSeenMu.Lock()
		delete(w.firstSeen, key)
		w.firstSeenMu.Unlock()
	}

	return actions
}

// starrDeleteOpts maps a stuck action to starr queue-delete options. The item is
// always removed from AltMount's download client. blocklist blocks the release;
// SkipRedownload is false only for blocklist_search so the *arr re-searches.
func starrDeleteOpts(action string) *starr.QueueDeleteOpts {
	removeFromClient := true
	blocklist := action == config.StuckActionBlocklist || action == config.StuckActionBlocklistSearch
	search := action == config.StuckActionBlocklistSearch
	return &starr.QueueDeleteOpts{
		RemoveFromClient: &removeFromClient,
		BlockList:        blocklist,
		SkipRedownload:   !search,
	}
}

// deleteStarrQueue runs the per-item starr delete (per its action) and counts
// successes, tolerating already-removed (404) items.
func (w *Worker) deleteStarrQueue(ctx context.Context, instance *model.ConfigInstance, actions []stuckAction, del func(ctx context.Context, id int64, opts *starr.QueueDeleteOpts) error) int {
	cleaned := 0
	for _, a := range actions {
		if err := del(ctx, a.ID, starrDeleteOpts(a.Action)); err != nil {
			if strings.Contains(err.Error(), "404") {
				slog.DebugContext(ctx, "Stuck queue item already removed", "instance", instance.Name, "id", a.ID)
				continue
			}
			slog.ErrorContext(ctx, "Failed to clean stuck queue item",
				"instance", instance.Name, "id", a.ID, "action", a.Action, "error", err)
			continue
		}
		cleaned++
	}
	if cleaned > 0 {
		slog.InfoContext(ctx, "Cleaned stuck imports",
			"instance", instance.Name, "type", instance.Type, "count", cleaned)
	}
	return cleaned
}

// flattenStarrMessages collapses starr status messages (title + lines) into a flat
// slice for pattern matching.
func flattenStarrMessages(msgs []*starr.StatusMessage) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		if m == nil {
			continue
		}
		if m.Title != "" {
			out = append(out, m.Title)
		}
		out = append(out, m.Messages...)
	}
	return out
}

func (w *Worker) cleanupStuckRadarr(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, force bool) (int, error) {
	client, err := w.clients.GetOrCreateRadarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get Radarr client: %w", err)
	}
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return 0, fmt.Errorf("failed to get Radarr queue: %w", err)
	}

	items := make([]stuckItem, 0, len(queue.Records))
	for _, q := range queue.Records {
		items = append(items, stuckItem{
			ID:                    q.ID,
			Title:                 q.Title,
			TrackedDownloadStatus: q.TrackedDownloadStatus,
			TrackedDownloadState:  q.TrackedDownloadState,
			DownloadClient:        q.DownloadClient,
			Messages:              flattenStarrMessages(q.StatusMessages),
		})
	}

	actions := w.selectStuckActions(ctx, instance, cfg, items, force)
	return w.deleteStarrQueue(ctx, instance, actions, client.DeleteQueueContext), nil
}

// cleanupStuckSonarr handles Sonarr and Whisparr (both use the Sonarr client).
func (w *Worker) cleanupStuckSonarr(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, force bool, whisparr bool) (int, error) {
	var (
		client *sonarr.Sonarr
		err    error
	)
	if whisparr {
		client, err = w.clients.GetOrCreateWhisparrClient(instance.Name, instance.URL, instance.APIKey)
	} else {
		client, err = w.clients.GetOrCreateSonarrClient(instance.Name, instance.URL, instance.APIKey)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get Sonarr client: %w", err)
	}
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return 0, fmt.Errorf("failed to get Sonarr queue: %w", err)
	}

	items := make([]stuckItem, 0, len(queue.Records))
	for _, q := range queue.Records {
		items = append(items, stuckItem{
			ID:                    q.ID,
			Title:                 q.Title,
			TrackedDownloadStatus: q.TrackedDownloadStatus,
			TrackedDownloadState:  q.TrackedDownloadState,
			DownloadClient:        q.DownloadClient,
			Messages:              flattenStarrMessages(q.StatusMessages),
		})
	}

	actions := w.selectStuckActions(ctx, instance, cfg, items, force)
	return w.deleteStarrQueue(ctx, instance, actions, client.DeleteQueueContext), nil
}

func (w *Worker) cleanupStuckLidarr(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, force bool) (int, error) {
	client, err := w.clients.GetOrCreateLidarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get Lidarr client: %w", err)
	}
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return 0, fmt.Errorf("failed to get Lidarr queue: %w", err)
	}

	items := make([]stuckItem, 0, len(queue.Records))
	for _, q := range queue.Records {
		// Lidarr's queue record has no trackedDownloadState; leave it empty (it is
		// not used for gating — only trackedDownloadStatus and the rule match are).
		items = append(items, stuckItem{
			ID:                    q.ID,
			Title:                 q.Title,
			TrackedDownloadStatus: q.TrackedDownloadStatus,
			DownloadClient:        q.DownloadClient,
			Messages:              flattenStarrMessages(q.StatusMessages),
		})
	}

	actions := w.selectStuckActions(ctx, instance, cfg, items, force)
	return w.deleteStarrQueue(ctx, instance, actions, client.DeleteQueueContext), nil
}

func (w *Worker) cleanupStuckReadarr(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, force bool) (int, error) {
	client, err := w.clients.GetOrCreateReadarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get Readarr client: %w", err)
	}
	queue, err := client.GetQueueContext(ctx, 0, 500)
	if err != nil {
		return 0, fmt.Errorf("failed to get Readarr queue: %w", err)
	}

	items := make([]stuckItem, 0, len(queue.Records))
	for _, q := range queue.Records {
		items = append(items, stuckItem{
			ID:                    q.ID,
			Title:                 q.Title,
			TrackedDownloadStatus: q.TrackedDownloadStatus,
			TrackedDownloadState:  q.TrackedDownloadState,
			DownloadClient:        q.DownloadClient,
			Messages:              flattenStarrMessages(q.StatusMessages),
		})
	}

	actions := w.selectStuckActions(ctx, instance, cfg, items, force)
	return w.deleteStarrQueue(ctx, instance, actions, client.DeleteQueueContext), nil
}

// cleanupStuckSportarr mirrors the starr path but uses Sportarr's native client,
// which is not starr-compatible.
func (w *Worker) cleanupStuckSportarr(ctx context.Context, instance *model.ConfigInstance, cfg *config.Config, force bool) (int, error) {
	client, err := w.clients.GetOrCreateSportarrClient(instance.Name, instance.URL, instance.APIKey)
	if err != nil {
		return 0, fmt.Errorf("failed to get Sportarr client: %w", err)
	}
	queue, err := client.GetQueue(ctx)
	if err != nil {
		return 0, fmt.Errorf("failed to get Sportarr queue: %w", err)
	}

	items := make([]stuckItem, 0, len(queue))
	for _, q := range queue {
		var messages []string
		for _, m := range q.StatusMessages {
			if m.Title != "" {
				messages = append(messages, m.Title)
			}
			messages = append(messages, m.Messages...)
		}
		items = append(items, stuckItem{
			ID:                    q.ID,
			Title:                 q.Title,
			TrackedDownloadStatus: q.TrackedDownloadStatus,
			TrackedDownloadState:  q.TrackedDownloadState,
			DownloadClient:        q.DownloadClient.Name,
			Messages:              messages,
		})
	}

	actions := w.selectStuckActions(ctx, instance, cfg, items, force)
	cleaned := 0
	for _, a := range actions {
		var err error
		// Sportarr's native API has no skipRedownload flag, so blocklist and
		// blocklist_search both map to a blocklisting delete.
		if a.Action == config.StuckActionBlocklist || a.Action == config.StuckActionBlocklistSearch {
			err = client.DeleteQueueItemBlocklist(ctx, a.ID)
		} else {
			err = client.DeleteQueueItem(ctx, a.ID)
		}
		if err != nil {
			if strings.Contains(err.Error(), "404") {
				slog.DebugContext(ctx, "Stuck queue item already removed from Sportarr", "instance", instance.Name, "id", a.ID)
				continue
			}
			slog.ErrorContext(ctx, "Failed to clean stuck Sportarr queue item",
				"instance", instance.Name, "id", a.ID, "action", a.Action, "error", err)
			continue
		}
		cleaned++
	}
	if cleaned > 0 {
		slog.InfoContext(ctx, "Cleaned stuck Sportarr imports", "instance", instance.Name, "count", cleaned)
	}
	return cleaned, nil
}
