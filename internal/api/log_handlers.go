package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
)

// LogEntry represents a single structured log entry from the JSON log file.
type LogEntry struct {
	Time    time.Time      `json:"time"`
	Level   string         `json:"level"`
	Message string         `json:"msg"`
	Attrs   map[string]any `json:"attrs,omitempty"`
}

// handleGetLogs handles GET /api/logs?level=info&limit=200
func (s *Server) handleGetLogs(c *fiber.Ctx) error {
	if s.logFilePath == "" {
		return RespondSuccess(c, []LogEntry{})
	}

	limit := c.QueryInt("limit", 200)
	levelFilter := strings.ToLower(c.Query("level"))

	entries, err := readLastLogLines(s.logFilePath, limit, levelFilter)
	if err != nil {
		if os.IsNotExist(err) {
			return RespondSuccess(c, []LogEntry{})
		}
		slog.ErrorContext(c.Context(), "failed to read log file", "error", err)
		return RespondInternalError(c, "Failed to read log file", err.Error())
	}

	return RespondSuccess(c, entries)
}

// ServeLogsSSE is a native net/http SSE handler for GET /api/logs/stream.
// It bypasses adaptor.FiberApp which cannot stream responses (blocks on
// Response.Body() reading the SSE pipe until EOF that never comes).
func (s *Server) ServeLogsSSE(w http.ResponseWriter, r *http.Request) {
	// Replicate RequireAuth logic for net/http requests.
	loginRequired := true
	if cfg := s.configManager.GetConfig(); cfg != nil && cfg.Auth.LoginRequired != nil {
		loginRequired = *cfg.Auth.LoginRequired
	}
	if loginRequired && s.authService != nil {
		if ts := s.authService.TokenService(); ts != nil {
			if _, _, err := ts.Get(r); err != nil {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}
		}
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	logPath := s.logFilePath

	// Send last 50 lines as initial payload.
	initial, _ := readLastLogLines(logPath, 50, "")
	if initial == nil {
		initial = []LogEntry{}
	}
	if data, err := json.Marshal(map[string]any{"type": "initial", "data": initial}); err == nil {
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	if logPath == "" {
		keepAlive := time.NewTicker(30 * time.Second)
		defer keepAlive.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-keepAlive.C:
				fmt.Fprintf(w, ": keep-alive\n\n")
				flusher.Flush()
			}
		}
	}

	// Record current end-of-file position so we only stream future writes.
	var lastPos int64
	if f, err := os.Open(logPath); err == nil {
		lastPos, _ = f.Seek(0, io.SeekEnd)
		f.Close()
	}

	poll := time.NewTicker(500 * time.Millisecond)
	defer poll.Stop()
	keepAlive := time.NewTicker(30 * time.Second)
	defer keepAlive.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-poll.C:
			newPos, entries := readNewLogLinesByPath(logPath, lastPos)
			lastPos = newPos
			for _, entry := range entries {
				if data, err := json.Marshal(map[string]any{"type": "update", "data": entry}); err == nil {
					fmt.Fprintf(w, "data: %s\n\n", data)
					flusher.Flush()
				}
			}
		case <-keepAlive.C:
			fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

// readLastLogLines reads the last `limit` log entries from the file,
// newest-first, optionally filtered by level.
func readLastLogLines(path string, limit int, levelFilter string) ([]LogEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var lines []string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			lines = append(lines, line)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Reverse so newest is first.
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}

	var entries []LogEntry
	for _, line := range lines {
		if len(entries) >= limit {
			break
		}
		entry, ok := parseLogLine(line)
		if !ok {
			continue
		}
		if levelFilter != "" && strings.ToLower(entry.Level) != levelFilter {
			continue
		}
		entries = append(entries, entry)
	}

	return entries, nil
}

// readNewLogLinesByPath opens the log file at path, reads any new lines since
// lastPos, and returns the updated position and parsed entries.
// Returns lastPos unchanged if the file cannot be opened (e.g. not yet created).
func readNewLogLinesByPath(path string, lastPos int64) (int64, []LogEntry) {
	f, err := os.Open(path)
	if err != nil {
		return lastPos, nil
	}
	defer f.Close()
	return readNewLogLines(f, lastPos)
}

// readNewLogLines reads bytes appended to f since lastPos.
func readNewLogLines(f *os.File, lastPos int64) (int64, []LogEntry) {
	fi, err := f.Stat()
	if err != nil {
		return lastPos, nil
	}

	newSize := fi.Size()

	// Handle log rotation (file shrunk).
	if newSize < lastPos {
		lastPos = 0
	}

	if newSize <= lastPos {
		return lastPos, nil
	}

	if _, err := f.Seek(lastPos, io.SeekStart); err != nil {
		return lastPos, nil
	}

	buf := make([]byte, newSize-lastPos)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return lastPos, nil
	}

	var entries []LogEntry
	for _, raw := range bytes.Split(buf[:n], []byte("\n")) {
		raw = bytes.TrimSpace(raw)
		if len(raw) == 0 {
			continue
		}
		if entry, ok := parseLogLine(string(raw)); ok {
			entries = append(entries, entry)
		}
	}

	return lastPos + int64(n), entries
}

// parseLogLine parses a single JSON log line into a LogEntry.
func parseLogLine(line string) (LogEntry, bool) {
	var raw map[string]any
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return LogEntry{}, false
	}

	entry := LogEntry{
		Attrs: make(map[string]any),
	}

	if timeStr, ok := raw["time"].(string); ok {
		if t, err := time.Parse(time.RFC3339Nano, timeStr); err == nil {
			entry.Time = t
		} else if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
			entry.Time = t
		}
	}

	if level, ok := raw["level"].(string); ok {
		entry.Level = level
	}

	// slog uses "msg" by default; handler.go may rename it to "message".
	if msg, ok := raw["msg"].(string); ok {
		entry.Message = msg
	} else if msg, ok := raw["message"].(string); ok {
		entry.Message = msg
	}

	for k, v := range raw {
		switch k {
		case "time", "level", "msg", "message":
			// already extracted
		default:
			entry.Attrs[k] = v
		}
	}

	return entry, true
}
