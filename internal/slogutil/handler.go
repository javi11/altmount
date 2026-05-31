package slogutil

import (
	"context"
	"log/slog"
	"os"
	"slices"
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
