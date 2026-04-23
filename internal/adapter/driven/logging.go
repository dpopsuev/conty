package driven

import (
	"context"
	"log/slog"
)

const (
	LogKeyBackend = "backend"
	LogKeyOp      = "op"
	LogKeyID      = "id"
	LogKeyElapsed = "elapsed"
	LogKeyCount   = "count"
)

func LogOp(ctx context.Context, backend, op string, attrs ...slog.Attr) {
	args := []slog.Attr{
		slog.String(LogKeyBackend, backend),
		slog.String(LogKeyOp, op),
	}
	args = append(args, attrs...)
	slog.LogAttrs(ctx, slog.LevelDebug, "adapter op", args...)
}

func LogOpDone(ctx context.Context, backend, op string, attrs ...slog.Attr) {
	args := []slog.Attr{
		slog.String(LogKeyBackend, backend),
		slog.String(LogKeyOp, op),
	}
	args = append(args, attrs...)
	slog.LogAttrs(ctx, slog.LevelDebug, "adapter op done", args...)
}

func LogError(ctx context.Context, backend, op string, err error) {
	slog.LogAttrs(ctx, slog.LevelError, "adapter error",
		slog.String(LogKeyBackend, backend),
		slog.String(LogKeyOp, op),
		slog.String("error", err.Error()),
	)
}
