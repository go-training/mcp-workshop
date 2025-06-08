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
	projectRoot string
}

func (h *callerHandler) Handle(ctx context.Context, r slog.Record) error {
	// skip 3 stack frames to get the actual caller of the log function
	_, file, line, ok := runtime.Caller(3)
	caller := ""
	if ok {
		relPath := file
		if strings.HasPrefix(file, h.projectRoot) {
			relPath = file[len(h.projectRoot):]
			// Remove leading slash if present
			relPath = strings.TrimPrefix(relPath, "/")
		}
		caller = fmt.Sprintf("%s:%d", relPath, line)
	} else {
		caller = "unknown"
	}
	r.AddAttrs(slog.String("caller", caller))
	return h.Handler.Handle(ctx, r)
}

// New initializes the default logger for the application.
// It uses text format and DEBUG level for development, JSON and INFO for production.
func New() {
	var handler slog.Handler
	handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	if os.Getenv("ENV") == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: slog.LevelInfo,
		})
	}
	// Get project root directory
	projectRoot, err := os.Getwd()
	if err != nil {
		projectRoot = ""
	}
	// Wrap with callerHandler to inject caller info
	handler = &callerHandler{
		Handler:     handler,
		projectRoot: projectRoot,
	}
	slog.SetDefault(slog.New(handler))
}
