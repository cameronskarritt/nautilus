package enums

type WebhookEventType string

const (
	WebhookEventTypeDocumentAvailable WebhookEventType = "document.available"
	WebhookEventTypeWebhookTest       WebhookEventType = "webhook.test"
)

func (t WebhookEventType) IsValid() bool {
	return t == WebhookEventTypeDocumentAvailable || t == WebhookEventTypeWebhookTest
}

type DeliveryStatus string

const (
	DeliveryStatusPending    DeliveryStatus = "pending"
	DeliveryStatusDelivering DeliveryStatus = "delivering"
	DeliveryStatusSucceeded  DeliveryStatus = "succeeded"
	DeliveryStatusFailed     DeliveryStatus = "failed"
	DeliveryStatusCanceled   DeliveryStatus = "canceled"
)

func (s DeliveryStatus) IsTerminal() bool {
	return s == DeliveryStatusSucceeded || s == DeliveryStatusFailed || s == DeliveryStatusCanceled
}

type DeliveryTrigger string

const (
	DeliveryTriggerEvent  DeliveryTrigger = "event"
	DeliveryTriggerReplay DeliveryTrigger = "replay"
)

type WebhookErrorCode string

const (
	WebhookErrorTimeout            WebhookErrorCode = "timeout"
	WebhookErrorNetwork            WebhookErrorCode = "network_error"
	WebhookErrorInvalidDestination WebhookErrorCode = "invalid_destination"
	WebhookErrorInvalidSecret      WebhookErrorCode = "invalid_secret"
)

func (c WebhookErrorCode) IsValid() bool {
	return c == WebhookErrorTimeout || c == WebhookErrorNetwork || c == WebhookErrorInvalidDestination || c == WebhookErrorInvalidSecret
}
