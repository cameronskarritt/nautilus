package failure

import (
	"testing"

	"go.temporal.io/api/common/v1"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/failure/v1"
	"go.temporal.io/sdk/temporal"
	"google.golang.org/protobuf/proto"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestApplicationClassification(t *testing.T) {
	t.Parallel()
	for _, nonRetryable := range []bool{false, true} {
		name := "retryable"
		if nonRetryable {
			name = "nonretryable"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := New("safe failure", "WebhookUnavailable", nonRetryable)
			var own *Error
			require.ErrorAs(t, err, &own)
			require.Equal(t, "safe failure", own.Error())
			var native *temporal.ApplicationError
			require.ErrorAs(t, err, &native)
			require.Equal(t, nonRetryable, native.NonRetryable())
			c := NewConverter()
			result := c.ErrorToFailure(errors.Wrap(err, "send webhook"))
			require.Equal(t, "send webhook: safe failure", result.Message)
			info := result.GetApplicationFailureInfo()
			require.NotNil(t, info)
			require.Equal(t, "WebhookUnavailable", info.Type)
			require.Equal(t, nonRetryable, info.NonRetryable)
			require.Nil(t, result.Cause)
			restored := c.FailureToError(result)
			require.ErrorAs(t, restored, &native)
			require.Equal(t, "WebhookUnavailable", native.Type())
			require.Equal(t, nonRetryable, native.NonRetryable())
		})
	}
}

func TestWrappedOrdinaryFailure(t *testing.T) {
	t.Parallel()
	c := NewConverter()
	err := errors.Wrap(errors.New("storage unavailable"), "read webhook")
	result := c.ErrorToFailure(err)
	require.Equal(t, "read webhook: storage unavailable", result.Message)
	require.False(t, result.GetApplicationFailureInfo().NonRetryable)
	require.ErrorContains(t, c.FailureToError(result), "read webhook: storage unavailable")
	require.Nil(t, c.ErrorToFailure(nil))
	require.Nil(t, c.FailureToError(nil))
}

func TestWrappedNativeMetadata(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		original *failure.Failure
	}{
		{
			name: "activity",
			original: &failure.Failure{
				Message: "activity failed",
				FailureInfo: &failure.Failure_ActivityFailureInfo{ActivityFailureInfo: &failure.ActivityFailureInfo{
					ScheduledEventId: 11, StartedEventId: 12, Identity: "worker", ActivityType: &common.ActivityType{Name: "SendWebhookAttempt"}, ActivityId: "attempt-1", RetryState: enums.RETRY_STATE_NON_RETRYABLE_FAILURE,
				}},
				Cause: &failure.Failure{Message: "invalid input", FailureInfo: &failure.Failure_ApplicationFailureInfo{ApplicationFailureInfo: &failure.ApplicationFailureInfo{Type: "InvalidWebhook", NonRetryable: true}}},
			},
		},
		{
			name:     "cancellation",
			original: &failure.Failure{Message: "canceled", FailureInfo: &failure.Failure_CanceledFailureInfo{CanceledFailureInfo: &failure.CanceledFailureInfo{}}},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := NewConverter()
			native := c.FailureToError(tt.original)
			saved := proto.Clone(c.ErrorToFailure(native))
			wrapped := errors.Wrap(native, "workflow stopped")
			result := c.ErrorToFailure(wrapped)
			require.Equal(t, wrapped.Error(), result.Message)
			expected := proto.Clone(tt.original).(*failure.Failure)
			expected.Message = wrapped.Error()
			require.True(t, proto.Equal(expected, result))
			require.True(t, proto.Equal(saved, c.ErrorToFailure(native)))
			restored := c.FailureToError(result)
			if tt.name == "activity" {
				var activity *temporal.ActivityError
				require.ErrorAs(t, restored, &activity)
				require.Equal(t, "attempt-1", activity.ActivityID())
				var application *temporal.ApplicationError
				require.ErrorAs(t, restored, &application)
				require.True(t, application.NonRetryable())
			} else {
				require.True(t, temporal.IsCanceledError(restored))
			}
		})
	}
}
