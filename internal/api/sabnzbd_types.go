package api

import (
	"fmt"
	"path/filepath"

	"github.com/javi11/altmount/internal/database"
	"github.com/javi11/altmount/internal/progress"
)

// SABnzbd-compatible API response structures
// These types match the expected response format from SABnzbd API

// SABnzbdResponse represents the standard SABnzbd API response wrapper
type SABnzbdResponse struct {
	Status  bool        `json:"status"`
	Queue   interface{} `json:"queue,omitempty"`
	History interface{} `json:"history,omitempty"`
	Config  interface{} `json:"config,omitempty"`
	Version interface{} `json:"version,omitempty"`
	Error   *string     `json:"error,omitempty"`
}

// SABnzbdQueueObject represents the nested queue object in the response
type SABnzbdQueueObject struct {
	Paused bool               `json:"paused"`
	Slots  []SABnzbdQueueSlot `json:"slots"`
}

// SABnzbdQueueResponse represents the queue response structure
type SABnzbdQueueResponse struct {
	Status bool               `json:"status"`
	Queue  SABnzbdQueueObject `json:"queue"`
}

// SABnzbdQueueSlot represents a single item in the download queue
type SABnzbdQueueSlot struct {
	Index      int    `json:"index"`
	NzoID      string `json:"nzo_id"`
	Priority   string `json:"priority"`
	Filename   string `json:"filename"`
	Cat        string `json:"cat"`
	Percentage string `json:"percentage"`
	Status     string `json:"status"`
	Timeleft   string `json:"timeleft"`
	Mb         string `json:"mb"`
	Mbleft     string `json:"mbleft"`
}

// SABnzbdHistoryResponse represents the history response structure
type SABnzbdHistoryResponse struct {
	Status    bool                 `json:"status"`
	Version   string               `json:"version"`
	Paused    bool                 `json:"paused"`
	NoOfSlots int                  `json:"noofslots"`
	Slots     []SABnzbdHistorySlot `json:"slots"`
	TotalSize string               `json:"total_size"`
	MonthSize string               `json:"month_size"`
	WeekSize  string               `json:"week_size"`
	DaySize   string               `json:"day_size"`
}

// SABnzbdHistorySlot represents a single item in the download history
type SABnzbdHistorySlot struct {
	Index        int      `json:"index"`
	NzoID        string   `json:"nzo_id"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	PP           string   `json:"pp"`
	Script       string   `json:"script"`
	Report       string   `json:"report"`
	URL          string   `json:"url"`
	Status       string   `json:"status"`
	NzbName      string   `json:"nzb_name"`
	Download     string   `json:"download"`
	Path         string   `json:"path"`
	Storage      string   `json:"storage"`
	Postproc     string   `json:"postproc"`
	Downloaded   int64    `json:"downloaded"`
	Completetime int64    `json:"completetime"`
	NzbAvg       string   `json:"nzb_avg"`
	Script_log   string   `json:"script_log"`
	Script_line  string   `json:"script_line"`
	DuplicateKey string   `json:"duplicate_key"`
	Fail_message string   `json:"fail_message"`
	Url_info     string   `json:"url_info"`
	Bytes        int64    `json:"bytes"`
	Meta         []string `json:"meta"`
	Series       string   `json:"series"`
	Md5sum       string   `json:"md5sum"`
	Password     string   `json:"password"`
	ActionLine   string   `json:"action_line"`
	Size         string   `json:"size"`
	Loaded       bool     `json:"loaded"`
	Retry        int      `json:"retry"`
	StateLog     []string `json:"stage_log"`
}

// SABnzbdStatusResponse represents the full status response
type SABnzbdStatusResponse struct {
	Status          bool    `json:"status"`
	Version         string  `json:"version"`
	Uptime          string  `json:"uptime"`
	Color           string  `json:"color"`
	Darwin          bool    `json:"darwin"`
	Nt              bool    `json:"nt"`
	Pid             int     `json:"pid"`
	NewRelURL       string  `json:"new_rel_url"`
	ActiveDownload  bool    `json:"active_download"`
	Paused          bool    `json:"paused"`
	PauseInt        int     `json:"pause_int"`
	Remaining       string  `json:"remaining"`
	MbLeft          float64 `json:"mbleft"`
	Diskspace1      string  `json:"diskspace1"`
	Diskspace2      string  `json:"diskspace2"`
	DiskspaceTotal1 string  `json:"diskspacetotal1"`
	DiskspaceTotal2 string  `json:"diskspacetotal2"`
	Loadavg         string  `json:"loadavg"`
	Cache           struct {
		Max  int `json:"max"`
		Left int `json:"left"`
		Art  int `json:"art"`
	} `json:"cache"`
	Folders []string           `json:"folders"`
	Slots   []SABnzbdQueueSlot `json:"slots"`
}

// SABnzbdConfig represents the SABnzbd configuration structure
type SABnzbdConfig struct {
	Misc       SABnzbdMiscConfig `json:"misc"`
	Categories []SABnzbdCategory `json:"categories"`
	Servers    []SABnzbdServer   `json:"servers"`
}

// SABnzbdMiscConfig represents miscellaneous configuration settings
type SABnzbdMiscConfig struct {
	CompleteDir            string `json:"complete_dir"`
	PreCheck               int    `json:"pre_check"`
	HistoryRetention       string `json:"history_retention"`
	HistoryRetentionOption string `json:"history_retention_option"`
	HistoryRetentionNumber int    `json:"history_retention_number"`
}

// SABnzbdCategory represents a download category configuration
type SABnzbdCategory struct {
	Name     string `json:"name"`
	Order    int    `json:"order"`
	PP       string `json:"pp"`
	Script   string `json:"script"`
	Dir      string `json:"dir"`
	Newzbin  string `json:"newzbin"`
	Priority int    `json:"priority"`
}

// SABnzbdServer represents a news server configuration
type SABnzbdServer struct {
	Name         string `json:"name"`
	DisplayName  string `json:"displayname"`
	Host         string `json:"host"`
	Port         int    `json:"port"`
	Timeout      int    `json:"timeout"`
	Username     string `json:"username"`
	Password     string `json:"password"`
	Connections  int    `json:"connections"`
	SSL          int    `json:"ssl"`
	SSLVerify    int    `json:"ssl_verify"`
	SSLCiphers   string `json:"ssl_ciphers"`
	Enable       int    `json:"enable"`
	Required     int    `json:"required"`
	Optional     int    `json:"optional"`
	Retention    int    `json:"retention"`
	ExpireDate   string `json:"expire_date"`
	Quota        string `json:"quota"`
	UsageAtStart int    `json:"usage_at_start"`
	Priority     int    `json:"priority"`
	Notes        string `json:"notes"`
}

// SABnzbdConfigResponse represents the configuration response
type SABnzbdConfigResponse struct {
	Status  bool          `json:"status"`
	Version string        `json:"version"`
	Config  SABnzbdConfig `json:"config"`
}

// SABnzbdVersionResponse represents the version response
type SABnzbdVersionResponse struct {
	Status  bool   `json:"status"`
	Version string `json:"version"`
}

// SABnzbdAddResponse represents the response from adding a download
type SABnzbdAddResponse struct {
	Status bool     `json:"status"`
	NzoIds []string `json:"nzo_ids,omitempty"`
	Error  *string  `json:"error,omitempty"`
}

// SABnzbdDeleteResponse represents the response from deleting an item
type SABnzbdDeleteResponse struct {
	Status bool    `json:"status"`
	Error  *string `json:"error,omitempty"`
}

// SABnzbdHistoryObject represents the nested history object in the complete response
type SABnzbdHistoryObject struct {
	Slots             []SABnzbdHistorySlot `json:"slots"`
	TotalSize         string               `json:"total_size"`
	MonthSize         string               `json:"month_size"`
	WeekSize          string               `json:"week_size"`
	DaySize           string               `json:"day_size"`
	Ppslots           int                  `json:"ppslots"`
	Noofslots         int                  `json:"noofslots"`
	LastHistoryUpdate int                  `json:"last_history_update"`
	Version           string               `json:"version"`
}

// SABnzbdCompleteHistoryResponse represents the complete history response structure
type SABnzbdCompleteHistoryResponse struct {
	History SABnzbdHistoryObject `json:"history"`
}

// Helper functions to convert AltMount data to SABnzbd format

// formatSizeMB formats bytes as megabytes string (like C# FormatSizeMB)
func formatSizeMB(bytes int64) string {
	if bytes == 0 {
		return "0.00"
	}
	megabytes := float64(bytes) / (1024.0 * 1024.0)
	return fmt.Sprintf("%.2f", megabytes)
}

// ToSABnzbdQueueSlot converts an AltMount ImportQueueItem to SABnzbd format
func ToSABnzbdQueueSlot(item *database.ImportQueueItem, index int, progressBroadcaster *progress.ProgressBroadcaster) SABnzbdQueueSlot {
	if item == nil {
		return SABnzbdQueueSlot{}
	}

	// Map AltMount status to SABnzbd status
	var status string
	switch item.Status {
	case database.QueueStatusPending:
		status = "Queued"
	case database.QueueStatusProcessing:
		status = "Downloading"
	case database.QueueStatusCompleted:
		status = "Completed"
	case database.QueueStatusFailed:
		status = "Failed"
	case database.QueueStatusPaused:
		status = "Paused"
	default:
		status = "Unknown"
	}

	// Map priority
	var priority string
	switch item.Priority {
	case database.QueuePriorityHigh:
		priority = "High"
	case database.QueuePriorityNormal:
		priority = "Normal"
	case database.QueuePriorityLow:
		priority = "Low"
	default:
		priority = "Normal"
	}

	// Extract filename from path
	filename := item.NzbPath
	if len(filename) > 0 {
		// Get just the filename from the full path
		if lastSlash := len(filename) - 1; lastSlash >= 0 {
			for i := lastSlash; i >= 0; i-- {
				if filename[i] == '/' || filename[i] == '\\' {
					filename = filename[i+1:]
					break
				}
			}
		}
	}

	// Get category, default to "default" if not set
	category := "default"
	if item.Category != nil && *item.Category != "" {
		category = *item.Category
	}

	// Calculate progress percentage using real-time progress broadcaster
	progressPercentage := 0
	switch item.Status {
	case database.QueueStatusProcessing:
		// Get real-time progress from progress broadcaster
		if progressBroadcaster != nil {
			if percentage, exists := progressBroadcaster.GetProgress(int(item.ID)); exists {
				progressPercentage = percentage
			} else {
				// Fallback to 50% if progress not tracked
				progressPercentage = 50
			}
		} else {
			// Fallback when broadcaster not available
			progressPercentage = 50
		}
	case database.QueueStatusCompleted:
		progressPercentage = 100
	}

	// Mock total size (could be enhanced to track actual file sizes)
	var totalSizeBytes int64
	if item.FileSize != nil {
		totalSizeBytes = *item.FileSize
	}

	sizeLeftBytes := int64((100 - progressPercentage) * int(totalSizeBytes) / 100)

	return SABnzbdQueueSlot{
		Index:      index,
		NzoID:      fmt.Sprintf("%d", item.ID),
		Priority:   priority,
		Filename:   filename,
		Cat:        category,
		Percentage: fmt.Sprintf("%d", progressPercentage),
		Status:     status,
		Timeleft:   "0:0:0:0", // Format: d:h:m:s
		Mb:         formatSizeMB(totalSizeBytes),
		Mbleft:     formatSizeMB(sizeLeftBytes),
	}
}

// ToSABnzbdHistorySlot converts an AltMount ImportQueueItem to SABnzbd history format
func ToSABnzbdHistorySlot(item *database.ImportQueueItem, index int, basePath string) SABnzbdHistorySlot {
	if item == nil {
		return SABnzbdHistorySlot{}
	}

	// Map AltMount status to SABnzbd history status
	var status string
	switch item.Status {
	case database.QueueStatusCompleted:
		status = "Completed"
	case database.QueueStatusFailed:
		status = "Failed"
	default:
		status = "Unknown"
	}

	// Extract filename from path
	filename := item.NzbPath
	name := filename
	if len(filename) > 0 {
		// Get just the filename from the full path
		if lastSlash := len(filename) - 1; lastSlash >= 0 {
			for i := lastSlash; i >= 0; i-- {
				if filename[i] == '/' || filename[i] == '\\' {
					name = filename[i+1:]
					break
				}
			}
		}
		// Remove .nzb extension for display name
		if len(name) > 4 && name[len(name)-4:] == ".nzb" {
			name = name[:len(name)-4]
		}
	}

	var completetime int64
	if item.CompletedAt != nil {
		completetime = item.CompletedAt.Unix()
	}

	failMessage := ""
	if item.ErrorMessage != nil {
		failMessage = *item.ErrorMessage
	}

	// Get category, default to "default" if not set
	category := "default"
	if item.Category != nil && *item.Category != "" {
		category = *item.Category
	}

	// Calculate storage path using the provided base path (which includes category folder)
	var storagePath string
	if item.StoragePath != nil {
		// Construct path: basePath/basename
		storagePath = filepath.Join(basePath, *item.StoragePath)
	} else {
		storagePath = basePath
	}

	return SABnzbdHistorySlot{
		Index:        index,
		NzoID:        fmt.Sprintf("%d", item.ID),
		Name:         name,
		Category:     category,
		PP:           "",
		Script:       "",
		Report:       "",
		URL:          "",
		Status:       status,
		NzbName:      filename,
		Download:     name,
		Storage:      storagePath,
		Path:         storagePath,
		Postproc:     "",
		Downloaded:   0,
		Completetime: completetime,
		NzbAvg:       "",
		Script_log:   "",
		DuplicateKey: name,
		Script_line:  "",
		Fail_message: failMessage,
		Url_info:     "",
		Bytes:        0,
		Meta:         []string{},
		Series:       "",
		Md5sum:       "",
		Password:     "",
		ActionLine:   "",
		Size:         "0 B",
		Loaded:       true,
		Retry:        item.RetryCount,
		StateLog:     []string{},
	}
}
