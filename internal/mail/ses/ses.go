package ses

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sesv2"
	"github.com/aws/aws-sdk-go-v2/service/sesv2/types"

	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/mail"
)

var _ mail.Sender = new(Sender)

type Sender struct {
	client           *sesv2.Client
	logger           *log.Logger
	configurationSet string
}

func NewSender(cfg aws.Config, logger *log.Logger, configurationSet string) *Sender {
	return &Sender{
		client:           sesv2.NewFromConfig(cfg),
		logger:           logger,
		configurationSet: configurationSet,
	}
}

func (s *Sender) Send(ctx context.Context, message *mail.Message) error {
	if message.From == "" {
		return mail.ErrMissingFrom
	}
	if len(message.To) == 0 {
		return mail.ErrMissingTo
	}
	if message.HTML == "" && message.Plaintext == "" {
		return mail.ErrMissingBody
	}

	input := &sesv2.SendEmailInput{
		FromEmailAddress: &message.From,
		Destination: &types.Destination{
			ToAddresses: message.To,
		},
		Content: &types.EmailContent{
			Simple: &types.Message{
				Subject: &types.Content{
					Data:    &message.Subject,
					Charset: aws.String("UTF-8"),
				},
				Body: &types.Body{},
			},
		},
	}

	if s.configurationSet != "" {
		input.ConfigurationSetName = &s.configurationSet
	}

	if message.HTML != "" {
		input.Content.Simple.Body.Html = &types.Content{
			Data:    &message.HTML,
			Charset: aws.String("UTF-8"),
		}
	}
	if message.Plaintext != "" {
		input.Content.Simple.Body.Text = &types.Content{
			Data:    &message.Plaintext,
			Charset: aws.String("UTF-8"),
		}
	}

	out, err := s.client.SendEmail(ctx, input)
	if err != nil {
		return classifyError(s.logger, err)
	}

	messageID := ""
	if out.MessageId != nil {
		messageID = *out.MessageId
	}

	s.logger.Info("sent email via SES", "message_id", messageID, "to", message.To)

	return nil
}

func classifyError(logger *log.Logger, err error) error {
	var (
		rejected     *types.MessageRejected
		suspended    *types.AccountSuspendedException
		paused       *types.SendingPausedException
		domainNotVer *types.MailFromDomainNotVerifiedException
		tooMany      *types.TooManyRequestsException
	)

	switch {
	case errors.As(err, &rejected):
		logger.Error("ses: message rejected", "error", err)
		return errors.Wrap(err, "ses: message rejected")
	case errors.As(err, &suspended):
		logger.Error("ses: account suspended", "error", err)
		return errors.Wrap(err, "ses: account suspended")
	case errors.As(err, &paused):
		logger.Error("ses: sending paused", "error", err)
		return errors.Wrap(err, "ses: sending paused")
	case errors.As(err, &domainNotVer):
		logger.Error("ses: mail-from domain not verified", "error", err)
		return errors.Wrap(err, "ses: mail-from domain not verified")
	case errors.As(err, &tooMany):
		logger.Warn("ses: throttled by SES", "error", err)
		return errors.Wrap(err, "ses: throttled")
	default:
		logger.Error("ses: failed to send email", "error", err)
		return errors.Wrap(err, "ses: failed to send email")
	}
}
