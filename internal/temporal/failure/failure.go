package failure

import (
	"go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"

	"nautilus/internal/errors"
)

type Error struct {
	message string
	cause   error
}

func New(message, kind string, nonRetryable bool) error {
	return &Error{
		message: message,
		cause:   temporal.NewApplicationErrorWithOptions(message, kind, temporal.ApplicationErrorOptions{NonRetryable: nonRetryable}),
	}
}

func (e *Error) Error() string { return e.message }
func (e *Error) Unwrap() error { return e.cause }

type failureConverter struct {
	converter.FailureConverter
}

func NewConverter() converter.FailureConverter {
	return &failureConverter{FailureConverter: temporal.GetDefaultFailureConverter()}
}

func (c *failureConverter) ErrorToFailure(err error) *failure.Failure {
	if err == nil {
		return nil
	}
	// pkg/errors.Cause removes context and stack wrappers without unwrapping SDK
	// activity/child failures, whose causes have distinct Temporal semantics.
	cause := errors.Cause(err)
	if own, ok := cause.(*Error); ok {
		cause = own.cause
	}
	result := c.FailureConverter.ErrorToFailure(cause)
	if _, wrapped := err.(interface{ Cause() error }); wrapped {
		// The default converter may return a cached SDK failure. Do not mutate it.
		result = proto.Clone(result).(*failure.Failure)
		result.Message = err.Error()
	}
	return result
}
