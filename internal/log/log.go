package log

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"nautilus/internal/config"
)

type Logger struct {
	*slog.Logger
}

func New(handler slog.Handler) *Logger {
	return &Logger{
		Logger: slog.New(handler),
	}
}

func (l *Logger) Fatal(message string, args ...any) {
	l.Error(message, args...)
	os.Exit(1)
}

func (l *Logger) WithGroup(name string) *Logger {
	if name == "" {
		return l
	}
	clone := *l
	clone.Logger = l.Logger.WithGroup(name)
	return &clone
}

func (l *Logger) With(args ...any) *Logger {
	clone := *l
	clone.Logger = l.Logger.With(args...)
	return &clone
}

type Options struct {
	Level slog.Leveler
}

var defaultLogger *Logger

func init() {
	defaultLogger = &Logger{
		Logger: slog.Default(),
	}
}

type contextKey struct{}

func Default() *Logger {
	return defaultLogger
}

func WithContext(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

func FromContext(ctx context.Context, fallback ...*Logger) *Logger {
	l, ok := ctx.Value(contextKey{}).(*Logger)
	if !ok {
		if len(fallback) > 0 {
			return fallback[0]
		}
		return Default()
	}

	return l
}

func WithFields(r *http.Request, args ...any) *http.Request {
	ctx := r.Context()
	l := FromContext(ctx)

	l = l.With(args...)
	ctx = WithContext(ctx, l)

	return r.WithContext(ctx)
}

func InferLogger(service string) *Logger {
	env := config.Get("APP_ENV", "development")

	if env == "development" {
		handler := NewColorizedHandler(os.Stdout, slog.LevelDebug)
		return New(handler).WithGroup(service)
	}

	handler := NewJSONHandler(os.Stdout, slog.LevelInfo)
	return New(handler).With("service", service)
}
