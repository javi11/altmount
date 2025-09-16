package slogutil

import (
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/javi11/altmount/internal/config"
	"gopkg.in/natefinch/lumberjack.v2"
)

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

// SetupLogRotation configures slog with log rotation using lumberjack
// If logConfig.File is empty, it logs to console only
// If logConfig.File is configured, it logs to both console and file
// Returns the configured logger
func SetupLogRotation(logConfig config.LogConfig) *slog.Logger {
	var writer io.Writer = os.Stdout

	// If log file is configured, set up dual logging (console + file with rotation)
	if logConfig.File != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   logConfig.File,
			MaxSize:    logConfig.MaxSize,    // MB
			MaxBackups: logConfig.MaxBackups, // number of old files
			MaxAge:     logConfig.MaxAge,     // days
			Compress:   logConfig.Compress,   // compress old files
		}
		// Use io.MultiWriter to write to both console and file
		writer = io.MultiWriter(os.Stdout, fileWriter)
	}

	// Determine log level (prefer new config.Log.Level over old config.LogLevel)
	level := logConfig.Level
	if level == "" {
		level = "info" // fallback default
	}

	// Create handler with the writer and level
	handler := slog.NewTextHandler(writer, &slog.HandlerOptions{
		Level: parseLevel(level),
	})

	// Wrap handler to support context data extraction
	wrappedHandler := WrapHandler(handler)

	return slog.New(wrappedHandler)
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
