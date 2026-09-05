package ses

import (
	"context"
	"log/slog"
	"testing"

	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mail"
	"nautilus/internal/testutil/require"
)

func TestSender_SendValidatesMessage(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name    string
		Message *mail.Message
		WantErr error
	}{
		{
			Name:    "missing from",
			Message: &mail.Message{To: []string{"user@example.com"}, Plaintext: "hello"},
			WantErr: mail.ErrMissingFrom,
		},
		{
			Name:    "missing to",
			Message: &mail.Message{From: "sender@example.com", Plaintext: "hello"},
			WantErr: mail.ErrMissingTo,
		},
		{
			Name:    "missing body",
			Message: &mail.Message{From: "sender@example.com", To: []string{"user@example.com"}},
			WantErr: mail.ErrMissingBody,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			err := new(Sender).Send(context.Background(), tt.Message)
			require.ErrorIs(t, err, tt.WantErr)
		})
	}
}

func TestClassifyError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name    string
		Err     error
		Message string
	}{
		{
			Name:    "message rejected",
			Err:     &types.MessageRejected{},
			Message: "ses: message rejected",
		},
		{
			Name:    "account suspended",
			Err:     &types.AccountSuspendedException{},
			Message: "ses: account suspended",
		},
		{
			Name:    "sending paused",
			Err:     &types.SendingPausedException{},
			Message: "ses: sending paused",
		},
		{
			Name:    "mail from domain not verified",
			Err:     &types.MailFromDomainNotVerifiedException{},
			Message: "ses: mail-from domain not verified",
		},
		{
			Name:    "throttled",
			Err:     &types.TooManyRequestsException{},
			Message: "ses: throttled",
		},
		{
			Name:    "unclassified",
			Err:     errors.New("boom"),
			Message: "ses: failed to send email",
		},
	}

	logger := log.New(slog.DiscardHandler)
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			err := classifyError(logger, tt.Err)
			require.Error(t, err)
			require.ErrorIs(t, err, tt.Err)
			require.Contains(t, err.Error(), tt.Message)
		})
	}
}
