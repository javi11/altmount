package api

import (
	"github.com/javi11/altmount/internal/database"
)

// SABnzbd-compatible API response structures
// These types match the expected response format from SABnzbd API

// SABnzbdResponse represents the standard SABnzbd API response wrapper
type SABnzbdResponse struct {
	Status bool        `json:"status"`
	Queue  interface{} `json:"queue,omitempty"`
	History interface{} `json:"history,omitempty"`
	Config interface{} `json:"config,omitempty"`
	Version interface{} `json:"version,omitempty"`
	Error  *string     `json:"error,omitempty"`
}

// SABnzbdQueueResponse represents the queue response structure
type SABnzbdQueueResponse struct {
	Status         bool                 `json:"status"`
	Version        string               `json:"version"`
	Paused         bool                 `json:"paused"`
	PauseInt       int                  `json:"pause_int"`
	SizeLeft       string               `json:"sizeleft"`
	Size           string               `json:"size"`
	Speed          string               `json:"speed"`
	SpeedLimit     string               `json:"speedlimit"`
	SpeedLimitAbs  string               `json:"speedlimit_abs"`
	NoOfSlots      int                  `json:"noofslots"`
	NoOfSlotsTotal int                  `json:"noofslots_total"`
	KbPerSec       string               `json:"kbpersec"`
	MbLeft         float64              `json:"mbleft"`
	Mb             float64              `json:"mb"`
	TimeLeft       string               `json:"timeleft"`
	ETA            string               `json:"eta"`
	Slots          []SABnzbdQueueSlot   `json:"slots"`
	Diskspace1     string               `json:"diskspace1"`
	Diskspace2     string               `json:"diskspace2"`
	DiskspaceTotal1 string              `json:"diskspacetotal1"`
	DiskspaceTotal2 string              `json:"diskspacetotal2"`
}

// SABnzbdQueueSlot represents a single item in the download queue
type SABnzbdQueueSlot struct {
	Index      int     `json:"index"`
	NzoID      string  `json:"nzo_id"`
	Unpackopts string  `json:"unpackopts"`
	Priority   string  `json:"priority"`
	Script     string  `json:"script"`
	Filename   string  `json:"filename"`
	Labels     []string `json:"labels"`
	Password   string  `json:"password"`
	Cat        string  `json:"cat"`
	Mbleft     float64 `json:"mbleft"`
	Mb         float64 `json:"mb"`
	Size       string  `json:"size"`
	Sizeleft   string  `json:"sizeleft"`
	Percentage string  `json:"percentage"`
	Eta        string  `json:"eta"`
	Timeleft   string  `json:"timeleft"`
	Status     string  `json:"status"`
}

// SABnzbdHistoryResponse represents the history response structure
type SABnzbdHistoryResponse struct {
	Status    bool                   `json:"status"`
	Version   string                 `json:"version"`
	Paused    bool                   `json:"paused"`
	NoOfSlots int                    `json:"noofslots"`
	Slots     []SABnzbdHistorySlot   `json:"slots"`
	TotalSize string                 `json:"total_size"`
	MonthSize string                 `json:"month_size"`
	WeekSize  string                 `json:"week_size"`
	DaySize   string                 `json:"day_size"`
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
	Postproc     string   `json:"postproc"`
	Downloaded   int64    `json:"downloaded"`
	Completetime int64    `json:"completetime"`
	NzbAvg       string   `json:"nzb_avg"`
	Script_log   string   `json:"script_log"`
	Script_line  string   `json:"script_line"`
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
}

// SABnzbdStatusResponse represents the full status response
type SABnzbdStatusResponse struct {
	Status         bool    `json:"status"`
	Version        string  `json:"version"`
	Uptime         string  `json:"uptime"`
	Color          string  `json:"color"`
	Darwin         bool    `json:"darwin"`
	Nt             bool    `json:"nt"`
	Pid            int     `json:"pid"`
	NewRelURL      string  `json:"new_rel_url"`
	ActiveDownload bool    `json:"active_download"`
	Paused         bool    `json:"paused"`
	PauseInt       int     `json:"pause_int"`
	Remaining      string  `json:"remaining"`
	MbLeft         float64 `json:"mbleft"`
	Diskspace1     string  `json:"diskspace1"`
	Diskspace2     string  `json:"diskspace2"`
	DiskspaceTotal1 string `json:"diskspacetotal1"`
	DiskspaceTotal2 string `json:"diskspacetotal2"`
	Loadavg        string  `json:"loadavg"`
	Cache          struct {
		Max    int `json:"max"`
		Left   int `json:"left"`
		Art    int `json:"art"`
	} `json:"cache"`
	Folders []string `json:"folders"`
	Slots   []SABnzbdQueueSlot `json:"slots"`
}

// SABnzbdConfigResponse represents the configuration response
type SABnzbdConfigResponse struct {
	Status  bool                        `json:"status"`
	Version string                      `json:"version"`
	Config  map[string]interface{}      `json:"config"`
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

// Helper functions to convert AltMount data to SABnzbd format

// ToSABnzbdQueueSlot converts an AltMount ImportQueueItem to SABnzbd format
func ToSABnzbdQueueSlot(item *database.ImportQueueItem, index int) SABnzbdQueueSlot {
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
	case database.QueueStatusRetrying:
		status = "Queued"
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

	return SABnzbdQueueSlot{
		Index:      index,
		NzoID:      string(rune(item.ID)), // Convert ID to string
		Unpackopts: "3",
		Priority:   priority,
		Script:     "", // No script support
		Filename:   filename,
		Labels:     []string{},
		Password:   "",
		Cat:        category,
		Mbleft:     0.0,
		Mb:         0.0,
		Size:       "0 B",
		Sizeleft:   "0 B",
		Percentage: "0",
		Eta:        "unknown",
		Timeleft:   "0:00:00",
		Status:     status,
	}
}

// ToSABnzbdHistorySlot converts an AltMount ImportQueueItem to SABnzbd history format
func ToSABnzbdHistorySlot(item *database.ImportQueueItem, index int) SABnzbdHistorySlot {
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

	return SABnzbdHistorySlot{
		Index:        index,
		NzoID:        string(rune(item.ID)),
		Name:         name,
		Category:     category,
		PP:           "",
		Script:       "",
		Report:       "",
		URL:          "",
		Status:       status,
		NzbName:      filename,
		Download:     name,
		Path:         item.NzbPath,
		Postproc:     "",
		Downloaded:   0,
		Completetime: completetime,
		NzbAvg:       "",
		Script_log:   "",
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
	}
}