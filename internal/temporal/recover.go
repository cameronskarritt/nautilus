package temporal

import (
	"context"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/observability/stacktrace"
)

// Recover must be deferred in the goroutine it protects.
func Recover(ctx context.Context, err *error) {
	p := recover()
	if p == nil {
		return
	}
	switch v := p.(type) {
	case error:
		*err = v
	case string:
		*err = errors.New(v)
	default:
		*err = errors.Errorf("panic: %v", v)
	}
	*err = errors.WithStack(*err)
	log.FromContext(ctx).Error("recovered from worker panic",
		"error", (*err).Error(), "stack", stacktrace.ExceptionStackTrace(*err))
	stacktrace.Capture(ctx, *err, nil)
}
