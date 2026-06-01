package slogutil

import (
	"context"
	"log/slog"
	"maps"
)

type data map[string]slog.Attr

func (d data) append(attrs ...slog.Attr) {
	for _, attr := range attrs {
		d[attr.Key] = attr
	}
}

type dataKey struct{}

func cloneData(ctx context.Context) data {
	d, ok := ctx.Value(dataKey{}).(data)
	if !ok {
		return data{}
	}

	return maps.Clone(d)
}

// WithAttrs returns a new context with the given attributes.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	if len(attrs) == 0 {
		return ctx
	}

	d := cloneData(ctx)
	d.append(attrs...)

	return context.WithValue(ctx, dataKey{}, d)
}

// With returns a new context with the given key-value pairs.
func With(ctx context.Context, kvargs ...any) context.Context {
	if len(kvargs) == 0 {
		return ctx
	}

	d := cloneData(ctx)

	var r slog.Record

	r.Add(kvargs...)

	r.Attrs(func(a slog.Attr) bool {
		d[a.Key] = a
		return true
	})

	return context.WithValue(ctx, dataKey{}, d)
}

// IterAttrs walks through the attributes in the context.
// The return value is compatible with iter.Seq[slog.Attr] to allow range func.
//
// Example:
//
//	for attr := range slogutil.IterAttrs(ctx) {
//		// DO SOMETHING
//	}
//
// Feature description: https://tip.golang.org/wiki/RangefuncExperiment
func IterAttrs(ctx context.Context) func(func(attr slog.Attr) bool) {
	return func(yield func(attr slog.Attr) bool) {
		d, ok := ctx.Value(dataKey{}).(data)
		if !ok {
			return
		}

		for _, v := range d {
			if !yield(v) {
				return
			}
		}
	}
}

type dataHook struct{}

func (dataHook) Run(ctx context.Context, r *slog.Record) {
	IterAttrs(ctx)(func(a slog.Attr) bool {
		r.AddAttrs(a)
		return true
	})
}
