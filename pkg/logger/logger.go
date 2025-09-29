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
		relPath := trimPathDepth(file, 2)
		caller = fmt.Sprintf("%s:%d", relPath, line)
	} else {
		caller = "unknown"
	}
	r.AddAttrs(slog.String("caller", caller))
	return h.Handler.Handle(ctx, r)
}

// parseLogLevel converts a string log level to slog.Level
func parseLogLevel(level string) slog.Level {
	switch strings.ToUpper(level) {
	case "DEBUG":
		return slog.LevelDebug
	case "INFO":
		return slog.LevelInfo
	case "WARN", "WARNING":
		return slog.LevelWarn
	case "ERROR":
		return slog.LevelError
	default:
		return slog.LevelInfo // default to INFO level
	}
}

// getLogLevel determines the log level from environment or parameter
func getLogLevel(levelParam string) slog.Level {
	// Priority: parameter > LOG_LEVEL env > ENV env > default
	if levelParam != "" {
		return parseLogLevel(levelParam)
	}

	if logLevel := os.Getenv("LOG_LEVEL"); logLevel != "" {
		return parseLogLevel(logLevel)
	}

	// Legacy support: production env defaults to INFO
	if os.Getenv("ENV") == "production" {
		return slog.LevelInfo
	}

	return slog.LevelDebug // default for development
}

// New initializes the default logger for the application.
// It uses text format and DEBUG level for development, JSON and INFO for production.
func New() *slog.Logger {
	return NewWithLevel("")
}

// NewWithLevel initializes the logger with a specific log level.
// If level is empty, it falls back to environment variables and defaults.
func NewWithLevel(level string) *slog.Logger {
	logLevel := getLogLevel(level)

	var handler slog.Handler
	if os.Getenv("ENV") == "production" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	}

	// Wrap with callerHandler to inject caller info
	handler = &callerHandler{
		Handler: handler,
	}
	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Log the current level for debugging
	logger.Info("Logger initialized", "level", logLevel.String())

	return logger
}
