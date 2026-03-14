package slogutil

import (
	"context"
	"log/slog"
	"os"
	"strings"

	"github.com/javi11/altmount/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

// multiHandler fans out slog records to multiple handlers.
type multiHandler struct {
	handlers []slog.Handler
}

func (m multiHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, h := range m.handlers {
		if h.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (m multiHandler) Handle(ctx context.Context, r slog.Record) error {
	for _, h := range m.handlers {
		if h.Enabled(ctx, r.Level) {
			if err := h.Handle(ctx, r.Clone()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m multiHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithAttrs(attrs)
	}
	return multiHandler{handlers: handlers}
}

func (m multiHandler) WithGroup(name string) slog.Handler {
	handlers := make([]slog.Handler, len(m.handlers))
	for i, h := range m.handlers {
		handlers[i] = h.WithGroup(name)
	}
	return multiHandler{handlers: handlers}
}

type Format string

type ReplaceAttrFunc func(groups []string, a slog.Attr) slog.Attr

type Config struct {
	Level       slog.Leveler
	ReplaceAttr ReplaceAttrFunc
	Hooks       []Hook
	AddSource   bool
	LogPath     string
}

var defaultConfig = Config{
	Level:   defaultLevel(),
	LogPath: "activity.log",
}

func mergeConfig(config ...Config) Config {
	if len(config) == 0 {
		return defaultConfig
	}

	cfg := config[0]

	if cfg.Level == nil {
		cfg.Level = defaultConfig.Level
	}

	if cfg.LogPath == "" {
		cfg.LogPath = defaultConfig.LogPath
	}

	return cfg
}

func defaultLevel() slog.Leveler {
	if v := os.Getenv("LOG_LEVEL"); v != "" {
		return parseLevel(v)
	}

	return slog.LevelInfo
}

func parseLevel(level string) slog.Leveler {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// GetLogFilePath returns the resolved log file path.
// If logConfig.File is empty, it returns the default "activity.log".
func GetLogFilePath(logConfig config.LogConfig) string {
	if logConfig.File != "" {
		return logConfig.File
	}
	return "activity.log"
}

// SetupLogRotation configures slog with log rotation using lumberjack.
// Always logs to both stdout (text format) and a file (JSON format).
// The file path defaults to "activity.log" if logConfig.File is empty.
func SetupLogRotation(logConfig config.LogConfig) *slog.Logger {
	logFile := GetLogFilePath(logConfig)

	fileWriter := &lumberjack.Logger{
		Filename:   logFile,
		MaxSize:    logConfig.MaxSize,    // MB
		MaxBackups: logConfig.MaxBackups, // number of old files
		MaxAge:     logConfig.MaxAge,     // days
		Compress:   logConfig.Compress,   // compress old files
	}

	// Determine log level
	level := logConfig.Level
	if level == "" {
		level = "info"
	}

	opts := &slog.HandlerOptions{
		Level: parseLevel(level),
	}

	// Text handler for human-readable stdout, JSON handler for machine-readable file.
	textHandler := slog.NewTextHandler(os.Stdout, opts)
	jsonHandler := slog.NewJSONHandler(fileWriter, opts)

	combined := multiHandler{handlers: []slog.Handler{textHandler, jsonHandler}}

	return slog.New(WrapHandler(combined))
}

// SetupLogRotationWithFallback sets up log rotation with backward compatibility
// It checks both new log config and legacy log_level setting
func SetupLogRotationWithFallback(logConfig config.LogConfig, legacyLogLevel string) *slog.Logger {
	// Use legacy log level if new config level is empty
	if logConfig.Level == "" && legacyLogLevel != "" {
		logConfig.Level = legacyLogLevel
	}

	return SetupLogRotation(logConfig)
}
