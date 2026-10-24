// SCOPE:core — Observability core: structured logging (slog)
package observability

import (
	"context"
	"log/slog"
	"os"
)

type ctxKey string

const loggerKey ctxKey = "obs.logger"

var baseLogger *slog.Logger

func init() {
	lvl := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "DEBUG" {
		lvl = slog.LevelDebug
	}
	baseLogger = slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
	// The default logger is the JSON one: every bare slog call anywhere
	// (including third-party libs that respect a custom default, like
	// Genkit) emits JSONL on stdout. Structured stdout is the rule —
	// nothing human-formatted may write here (see cmd/web, web/resources).
	slog.SetDefault(baseLogger)
}

func L() *slog.Logger {
	if baseLogger == nil {
		baseLogger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}
	return baseLogger
}

// With returns a context whose logger is enriched with attrs. It composes
// with any logger already on the context (via From), so successive calls
// and an injected handler used by tests — are preserved rather than dropped.
func With(ctx context.Context, args ...any) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	lg := From(ctx)
	if len(args) > 0 {
		lg = lg.With(args...)
	}
	return context.WithValue(ctx, loggerKey, lg)
}

func From(ctx context.Context) *slog.Logger {
	if ctx == nil {
		return L()
	}
	if lg, ok := ctx.Value(loggerKey).(*slog.Logger); ok {
		return lg
	}
	return L()
}

// Component attrs to classify by gogogo layers: core/plugin/feature + part
func WithComponent(part, layer string) []any {
	return []any{"component", part, "layer", layer}
}

func Info(ctx context.Context, msg string, args ...any) {
	From(ctx).Info(msg, args...)
}

func Debug(ctx context.Context, msg string, args ...any) {
	From(ctx).Debug(msg, args...)
}

func Error(ctx context.Context, msg string, args ...any) {
	From(ctx).Error(msg, args...)
}

func Warn(ctx context.Context, msg string, args ...any) {
	From(ctx).Warn(msg, args...)
}

func WithFields(ctx context.Context, fields map[string]any) context.Context {
	args := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		args = append(args, k, v)
	}
	return With(ctx, args...)
}
