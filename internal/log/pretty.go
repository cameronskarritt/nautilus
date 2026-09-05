package log

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"

	"nautilus/internal/errors"
	"nautilus/internal/log/color"
)

const TimeFormat = "15:04:05.0000"

var _ slog.Handler = new(colorizedHandler)

type colorizedHandler struct {
	level  slog.Leveler
	buf    *bytes.Buffer
	mu     *sync.Mutex
	writer io.Writer
	group  string
	attrs  []slog.Attr
}

func NewColorizedHandler(writer io.Writer, level slog.Leveler) slog.Handler {
	return &colorizedHandler{
		level:  level,
		buf:    &bytes.Buffer{},
		mu:     &sync.Mutex{},
		writer: writer,
		attrs:  []slog.Attr{},
	}
}

func (h *colorizedHandler) Enabled(_ context.Context, level slog.Level) bool {
	return h.level.Level() <= level
}

func (h *colorizedHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	newAttrs := make([]slog.Attr, len(h.attrs)+len(attrs))
	copy(newAttrs, h.attrs)
	copy(newAttrs[len(h.attrs):], attrs)
	return &colorizedHandler{
		level:  h.level,
		buf:    h.buf,
		mu:     h.mu,
		writer: h.writer,
		group:  h.group,
		attrs:  newAttrs,
	}
}

func (h *colorizedHandler) WithGroup(name string) slog.Handler {
	group := name
	if h.group != "" {
		group = strings.Join([]string{h.group, name}, ".")
	}

	return &colorizedHandler{
		level:  h.level,
		buf:    h.buf,
		mu:     h.mu,
		writer: h.writer,
		group:  group,
		attrs:  h.attrs,
	}
}

func (h *colorizedHandler) Handle(_ context.Context, record slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.buf.Reset()

	var level string

	switch record.Level {
	case slog.LevelError:
		level = color.Style("ERR", color.Red)
	case slog.LevelWarn:
		level = color.Style("WRN", color.Yellow)
	case slog.LevelInfo:
		level = color.Style("INF", color.Green)
	case slog.LevelDebug:
		level = color.Style("DBG", color.Magenta)
	default:
		level = color.Style("???", color.White)
	}

	timestamp := record.Time.Format(TimeFormat)

	h.buf.WriteString(timestamp)
	h.buf.WriteString(" [")
	h.buf.WriteString(level)
	h.buf.WriteString("] ")

	if h.group != "" {
		h.buf.WriteString(h.group)
		h.buf.WriteString(": ")
	}

	h.buf.WriteString(record.Message)

	attrs := make([]slog.Attr, 0, len(h.attrs)+record.NumAttrs())
	attrs = append(attrs, h.attrs...)
	record.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})

	var stacktrace errors.StackTrace
	if len(attrs) > 0 {
		h.buf.WriteString(" | ")
		for _, attr := range attrs {
			// Handle stacktraces _after_ writing the rest of our attributes
			if st, ok := attr.Value.Any().(errors.StackTracer); ok {
				stacktrace = st.StackTrace()
				h.buf.WriteString(color.Style(attr.Key, color.Cyan))
				h.buf.WriteString("=")
				fmt.Fprintf(h.buf, "%v", st.Error())
				h.buf.WriteString(" ")
				continue
			}

			h.buf.WriteString(color.Style(attr.Key, color.Cyan))
			h.buf.WriteString("=")
			fmt.Fprintf(h.buf, "%v", attr.Value)
			h.buf.WriteString(" ")
		}
	}
	h.buf.WriteString("\n")

	if stacktrace != nil {
		h.buf.WriteString("\n")
		for _, frame := range stacktrace {
			fmt.Fprintf(h.buf, "%+v\n", frame)
		}
		h.buf.WriteString("\n")
	}

	_, err := h.writer.Write(h.buf.Bytes())
	return errors.Wrap(err, "error writing log")
}
