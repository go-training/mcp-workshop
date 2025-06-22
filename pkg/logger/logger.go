package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
)

// New initializes the default logger for the application.
// It uses text format and DEBUG level for development, JSON and INFO for production.
type callerHandler struct {
	slog.Handler
}

// trimPathDepth keeps only the last n segments of the given path.
// Example: trimPathDepth("a/b/c/d.go", 3) => "b/c/d.go"
func trimPathDepth(path string, depth int) string {
	parts := strings.Split(path, string(os.PathSeparator))
	if len(parts) <= depth {
		return path
	}
	return strings.Join(parts[len(parts)-depth:], string(os.PathSeparator))
}

func (h *callerHandler) Handle(ctx context.Context, r slog.Record) error {
	// Skip 3 stack frames to get the actual caller of the log function
	_, file, line, ok := runtime.Caller(3)
	caller := ""
	if ok {
		// Always show only the last 3 segments of the file path for readability
		relPath := trimPathDepth(file, 3)
		caller = fmt.Sprintf("%s:%d", relPath, line)
	} else {
		caller = "unknown"
	}
	r.AddAttrs(slog.String("caller", caller))
	return h.Handler.Handle(ctx, r)
}

// New initializes the default logger for the application.
// It uses text format and DEBUG level for development, JSON and INFO for production.
func New() *slog.Logger {
	var handler slog.Handler
	handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	if os.Getenv("ENV") == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	// Wrap with callerHandler to inject caller info
	handler = &callerHandler{
		Handler: handler,
	}
	slog.SetDefault(slog.New(handler))
	return slog.Default()
}
