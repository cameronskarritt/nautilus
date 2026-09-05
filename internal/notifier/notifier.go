package notifier

import (
	"context"

	"nautilus/internal/errors"
)

var (
	ErrRecipientRequired = errors.New("recipient is required")
)

type Urgency string

const (
	UrgencyLow    Urgency = "low"
	UrgencyNormal Urgency = "normal"
	UrgencyHigh   Urgency = "high"
)

type Notifier interface {
	Notify(ctx context.Context, notification *Notification) error
}

type Notification struct {
	Recipient string
	Subject   string
	Body      string
	Urgency   Urgency
	Metadata  map[string]string
}
