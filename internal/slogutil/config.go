package slogutil

import (
	"log/slog"
	"os"
	"strings"
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
