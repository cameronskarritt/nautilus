package mail

import (
	"context"

	"nautilus/internal/config"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

var (
	ErrMissingFrom = errors.New("mail: from address is required")
	ErrMissingTo   = errors.New("mail: at least one recipient is required")
	ErrMissingBody = errors.New("mail: at least one of plaintext or html body is required")
)

var templs *EmailTemplates

func init() {
	t, err := NewTemplates()
	if err != nil {
		panic(err)
	}

	templs = t
}

type Message struct {
	From      string   `json:"from"`
	To        []string `json:"to"`
	Subject   string   `json:"subject"`
	Plaintext string   `json:"plaintext"`
	HTML      string   `json:"html"`
}

type Sender interface {
	Send(ctx context.Context, message *Message) error
}

func SendTemplate(ctx context.Context, sender Sender, recipient string, template enums.MailTemplate, data any) error {
	subject, err := GetSubject(template)
	if err != nil {
		return err
	}

	plaintext, html, err := templs.ExecuteTemplate(template.String(), data)
	if err != nil {
		return err
	}

	message := &Message{
		From:      config.Get[string]("MAIL_SENDER_ADDRESS"),
		To:        []string{recipient},
		Subject:   subject,
		Plaintext: plaintext,
		HTML:      html,
	}
	return sender.Send(ctx, message)
}
