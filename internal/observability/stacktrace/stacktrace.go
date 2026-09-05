package stacktrace

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"runtime/debug"

	"nautilus/internal/errors"
)

type StackTracer interface {
	Capture(ctx context.Context, err error, opts *CaptureOptions)
}

type Shutdowner interface {
	Shutdown(ctx context.Context) error
}

type CaptureOptions struct {
	Request *http.Request
}

type contextKey struct{}

func WithContext(ctx context.Context, tracer StackTracer) context.Context {
	return context.WithValue(ctx, contextKey{}, tracer)
}

func FromContext(ctx context.Context) StackTracer {
	tracer, _ := ctx.Value(contextKey{}).(StackTracer)
	return tracer
}

func Capture(ctx context.Context, err error, opts *CaptureOptions) {
	tracer := FromContext(ctx)
	if tracer == nil || err == nil {
		return
	}
	tracer.Capture(ctx, err, opts)
}

func ExceptionType(err error) string {
	cause := errors.Cause(err)
	if cause == nil {
		cause = err
	}
	if cause == nil {
		return ""
	}

	t := reflect.TypeOf(cause)
	if t == nil {
		return ""
	}
	return t.String()
}

func ExceptionStackTrace(err error) string {
	var st errors.StackTracer
	if errors.As(err, &st) {
		return fmt.Sprintf("%+v", st.StackTrace())
	}
	return string(debug.Stack())
}

func NewMulti(tracers ...StackTracer) StackTracer {
	filtered := make([]StackTracer, 0, len(tracers))
	for _, tracer := range tracers {
		if tracer != nil {
			filtered = append(filtered, tracer)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return multiStackTracer(filtered)
}

type multiStackTracer []StackTracer

func (m multiStackTracer) Capture(ctx context.Context, err error, opts *CaptureOptions) {
	for _, tracer := range m {
		tracer.Capture(ctx, err, opts)
	}
}

func (m multiStackTracer) Shutdown(ctx context.Context) error {
	for _, tracer := range m {
		shutdowner, ok := tracer.(Shutdowner)
		if !ok {
			continue
		}
		if err := shutdowner.Shutdown(ctx); err != nil {
			return err
		}
	}
	return nil
}
