package core

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/google/uuid"
)

// AuthKey is a custom context key type for storing the auth token in context.
type AuthKey struct{}

// RequestIDKey is a custom context key type for storing the request ID in context.
type RequestIDKey struct{}

// StoreKey is a custom context key type for storing the Store in context.
type StoreKey struct{}

// WithRequestID returns a new context with a generated request ID set.
func WithRequestID(ctx context.Context) context.Context {
	reqID := uuid.New().String()
	return context.WithValue(ctx, RequestIDKey{}, reqID)
}

// withAuthKey returns a new context with the provided auth token set.
func withAuthKey(ctx context.Context, auth string) context.Context {
	return context.WithValue(ctx, AuthKey{}, auth)
}

// authFromRequest extracts the Authorization header from the HTTP request
// and stores it in the context. Used for HTTP transport.
func AuthFromRequest(ctx context.Context, r *http.Request) context.Context {
	return withAuthKey(ctx, r.Header.Get("Authorization"))
}

// authFromEnv extracts the API_KEY environment variable and stores it in the context.
// Used for stdio transport.
func AuthFromEnv(ctx context.Context) context.Context {
	return withAuthKey(ctx, os.Getenv("API_KEY"))
}

// TokenFromContext retrieves the auth token from the context.
// Returns the token string if present, or an error if missing.
func TokenFromContext(ctx context.Context) (string, error) {
	auth, ok := ctx.Value(AuthKey{}).(string)
	if !ok {
		return "", fmt.Errorf("missing auth")
	}
	return auth, nil
}

// LoggerFromCtx returns a slog.Logger with request_id field if present in context.
// If no request ID is found, it returns the default logger.
// This allows for structured logging with request context.
func LoggerFromCtx(ctx context.Context) *slog.Logger {
	reqID, _ := ctx.Value(RequestIDKey{}).(string)
	if reqID != "" {
		return slog.Default().With("request_id", reqID)
	}
	return slog.Default()
}

// WithStore returns a new context with the provided Store set.
func WithStore(ctx context.Context, store Store) context.Context {
	return context.WithValue(ctx, StoreKey{}, store)
}

// StoreFromContext retrieves the Store from the context.
// Returns the Store interface if present, or an error if missing.
func StoreFromContext(ctx context.Context) (Store, error) {
	store, ok := ctx.Value(StoreKey{}).(Store)
	if !ok {
		return nil, fmt.Errorf("missing store")
	}
	return store, nil
}
