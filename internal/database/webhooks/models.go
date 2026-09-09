package webhooks

import (
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/optional"
)

type Webhook struct {
	ID                      int                          `json:"-"`
	ExternalID              string                       `json:"id"`
	OrganizationID          int                          `json:"-"`
	Name                    string                       `json:"name"`
	URL                     string                       `json:"url"`
	EventTypes              []enums.WebhookEventType     `json:"event_types"`
	Enabled                 bool                         `json:"enabled"`
	SigningSecret           []byte                       `json:"-"`
	PreviousSigningSecret   optional.Optional[[]byte]    `json:"-"`
	PreviousSecretExpiresAt optional.Optional[time.Time] `json:"-"`
	CreatedAt               time.Time                    `json:"created_at"`
	UpdatedAt               time.Time                    `json:"updated_at"`
	DeletedAt               optional.Optional[time.Time] `json:"-"`
}

type CreateOptions struct {
	ExternalID    string
	Name          string
	URL           string
	EventTypes    []enums.WebhookEventType
	SigningSecret []byte
}

type UpdateOptions struct {
	Name       optional.Optional[string]
	URL        optional.Optional[string]
	EventTypes optional.Optional[[]enums.WebhookEventType]
	Enabled    optional.Optional[bool]
}

type Delivery struct {
	ID                 int                          `json:"-"`
	ExternalID         string                       `json:"id"`
	OrganizationID     int                          `json:"-"`
	WebhookID          int                          `json:"-"`
	EventID            int                          `json:"-"`
	WebhookExternalID  string                       `json:"webhook_id"`
	EventExternalID    string                       `json:"event_id"`
	Trigger            enums.DeliveryTrigger        `json:"trigger"`
	ReplayedID         optional.Optional[int]       `json:"-"`
	ReplayedExternalID optional.Optional[string]    `json:"replayed_id"`
	RequestKey         string                       `json:"-"`
	Status             enums.DeliveryStatus         `json:"status"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	CompletedAt        optional.Optional[time.Time] `json:"completed_at"`
}

type Attempt struct {
	ID             int                                       `json:"id"`
	OrganizationID int                                       `json:"-"`
	DeliveryID     int                                       `json:"-"`
	AttemptKey     string                                    `json:"-"`
	URL            string                                    `json:"url"`
	StartedAt      time.Time                                 `json:"started_at"`
	FinishedAt     optional.Optional[time.Time]              `json:"finished_at"`
	HTTPStatus     optional.Optional[int]                    `json:"http_status"`
	ErrorCode      optional.Optional[enums.WebhookErrorCode] `json:"error_code"`
}
