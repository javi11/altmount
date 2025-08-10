package slogutil

import (
	"context"
	"io"
	"log/slog"
	"os"
	"slices"

	"github.com/natefinch/lumberjack"
)

// Hook is called when a slog record is handled.
type Hook interface {
	Run(ctx context.Context, r *slog.Record)
}

// Handler is a slog.Handler with hooks support.
type Handler struct {
	handler slog.Handler
	hooks   []Hook
}

// NewHandler creates a new Handler with the given configuration.
func NewHandler(config ...Config) Handler {
	cfg := mergeConfig(config...)

	replaceAttr := changeMsgKey(cfg.ReplaceAttr)

	base := slog.NewJSONHandler(io.MultiWriter(os.Stdout, &lumberjack.Logger{
		Filename:   cfg.LogPath,
		MaxSize:    5,
		MaxAge:     14,
		MaxBackups: 5,
	}), &slog.HandlerOptions{
		Level:       cfg.Level,
		AddSource:   cfg.AddSource,
		ReplaceAttr: replaceAttr,
	})

	return WrapHandler(base).WithHooks(cfg.Hooks...)
}

// WrapHandler creates a new Handler with the given slog.Handler.
// If the provided handler is nil, a default JSON handler is used.
func WrapHandler(h slog.Handler) Handler {
	if h == nil {
		h = slog.NewJSONHandler(os.Stdout, nil)
	}

	return Handler{
		handler: h,
		hooks: []Hook{
			dataHook{},
		},
	}
}

func (h Handler) Enabled(ctx context.Context, l slog.Level) bool {
	return h.handler.Enabled(ctx, l)
}

func (h Handler) Handle(ctx context.Context, r slog.Record) error {
	if len(h.hooks) > 0 {
		r = r.Clone()

		for _, hook := range h.hooks {
			hook.Run(ctx, &r)
		}
	}

	return h.handler.Handle(ctx, r)
}

func (h Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return Handler{
		hooks:   h.hooks,
		handler: h.handler.WithAttrs(attrs),
	}
}

func (h Handler) WithGroup(name string) slog.Handler {
	return Handler{
		hooks:   h.hooks,
		handler: h.handler.WithGroup(name),
	}
}

func (h Handler) WithHooks(hooks ...Hook) Handler {
	if len(hooks) == 0 {
		return h
	}

	return Handler{
		hooks:   slices.Concat(h.hooks, hooks),
		handler: h.handler,
	}
}

const MessageKey = "message"

func changeMsgKey(fn ReplaceAttrFunc) ReplaceAttrFunc {
	return func(groups []string, a slog.Attr) slog.Attr {
		if a.Key == slog.MessageKey {
			a = slog.String(MessageKey, a.Value.String())
		}

		if fn != nil {
			return fn(groups, a)
		}

		return a
	}
}
