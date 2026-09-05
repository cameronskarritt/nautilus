package tracer

import (
	"context"

	"nautilus/internal/mail"
)

var _ mail.Sender = (*TracedMailSender)(nil)

type TracedMailSender struct {
	sender mail.Sender
	tracer Tracer
}

func NewTracedMailSender(sender mail.Sender, t Tracer) *TracedMailSender {
	return &TracedMailSender{sender: sender, tracer: t}
}

func (s *TracedMailSender) Send(ctx context.Context, msg *mail.Message) error {
	ctx, span := s.tracer.Start(ctx, "mail.send")
	defer span.End()

	span.SetAttributes(StringAttr("mail.subject", msg.Subject))

	err := s.sender.Send(ctx, msg)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return err
}
