package errors

import (
	"github.com/pkg/errors"
)

var (
	New          = errors.New
	Errorf       = errors.Errorf
	WithStack    = errors.WithStack
	Wrap         = errors.Wrap
	Wrapf        = errors.Wrapf
	WithMessage  = errors.WithMessage
	WithMessagef = errors.WithMessagef
	Is           = errors.Is
	As           = errors.As
	Unwrap       = errors.Unwrap
	Cause        = errors.Cause
)

type StackTrace = errors.StackTrace

type StackTracer interface {
	Error() string
	StackTrace() StackTrace
}
