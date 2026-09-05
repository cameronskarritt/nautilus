package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"nautilus/internal/errors"
)

type jsonHandler struct {
	*slog.JSONHandler
}

func NewJSONHandler(w io.Writer, level slog.Leveler) slog.Handler {
	opts := &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			if st, ok := a.Value.Any().(errors.StackTracer); ok {
				stack := st.StackTrace()
				frames := make([]string, len(stack))
				for i, frame := range stack {
					frames[i] = fmt.Sprintf("%+v", frame)
				}
				return slog.Any(a.Key, frames)
			}
			return a
		},
	}

	return &jsonHandler{
		JSONHandler: slog.NewJSONHandler(w, opts),
	}
}

func (h *jsonHandler) Handle(ctx context.Context, r slog.Record) error {
	err := h.JSONHandler.Handle(ctx, r)
	if err != nil {
		return errors.Wrap(err, "error handling log record")
	}
	return nil
}

func (h *jsonHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &jsonHandler{
		JSONHandler: h.JSONHandler.WithAttrs(attrs).(*slog.JSONHandler),
	}
}

func (h *jsonHandler) WithGroup(name string) slog.Handler {
	return &jsonHandler{
		JSONHandler: h.JSONHandler.WithGroup(name).(*slog.JSONHandler),
	}
}

func (h *jsonHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.JSONHandler.Enabled(ctx, level)
}
