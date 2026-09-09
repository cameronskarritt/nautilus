package webhookdelivery

import (
	"context"
	"strconv"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/temporal"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/kms"
	"nautilus/internal/webhook"
)

type Activities struct {
	DB     database.Database
	Keys   kms.KeyManager
	Sender *webhook.Sender
	send   func(context.Context, string, string, []byte, [][]byte) webhook.Result
}

func (a Activities) Send(ctx context.Context, input Input) (enums.DeliveryStatus, error) {
	if err := input.normalize(); err != nil {
		return "", err
	}
	delivery, err := webhooks.GetDelivery(ctx, a.DB, input.OrganizationID, input.DeliveryID)
	if err != nil {
		return "", unavailable()
	}
	if delivery == nil {
		return currentStatus(ctx, a.DB, input)
	}
	if delivery.Status.IsTerminal() {
		return delivery.Status, nil
	}
	if !time.Now().Before(delivery.CreatedAt.Add(lifetime)) {
		return a.Complete(ctx, Completion{Input: input, Status: enums.DeliveryStatusFailed})
	}
	hook, err := webhooks.Get(ctx, a.DB, input.OrganizationID, delivery.WebhookID)
	if err != nil {
		return "", unavailable()
	}
	if hook == nil || !hook.Enabled {
		return a.Complete(ctx, Completion{Input: input, Status: enums.DeliveryStatusCanceled})
	}
	org, err := organizations.Get(ctx, a.DB, input.OrganizationID)
	if err != nil {
		return "", unavailable()
	}
	if org == nil {
		return currentStatus(ctx, a.DB, input)
	}
	event, err := webhookevents.GetByExternalID(ctx, a.DB, input.OrganizationID, delivery.EventExternalID)
	if err != nil || event == nil {
		return "", unavailable()
	}
	enc := encrypt.ForOrganization(a.Keys, org.ExternalID)
	binding := encrypt.Binding{Purpose: "webhook-signing-secret", RecordID: hook.ExternalID}
	secret, err := enc.Open(ctx, hook.SigningSecret, binding)
	defer clear(secret)
	if err != nil {
		return "", unavailable()
	}
	secrets := [][]byte{secret}
	if hook.PreviousSigningSecret.Set && hook.PreviousSecretExpiresAt.Set && hook.PreviousSecretExpiresAt.Data.After(time.Now()) {
		previous, err := enc.Open(ctx, hook.PreviousSigningSecret.Data, binding)
		defer clear(previous)
		if err != nil {
			return "", unavailable()
		}
		secrets = append(secrets, previous)
	}
	if _, err := webhooks.MarkDelivering(ctx, a.DB, input.OrganizationID, delivery.ID); err != nil {
		return "", unavailable()
	}
	info := activity.GetInfo(ctx)
	key := info.WorkflowExecution.RunID + ":" + info.ActivityID + ":" + strconv.Itoa(int(info.Attempt))
	attempt, err := webhooks.StartAttempt(ctx, a.DB, input.OrganizationID, delivery.ID, key, hook.URL)
	if err != nil {
		return "", unavailable()
	}
	if attempt == nil || attempt.FinishedAt.Set {
		return currentStatus(ctx, a.DB, input)
	}
	send := a.send
	if send == nil {
		if a.Sender == nil {
			return "", unavailable()
		}
		send = a.Sender.Send
	}
	if len(secrets) > 1 && !hook.PreviousSecretExpiresAt.Data.After(time.Now()) {
		secrets = secrets[:1]
	}
	result := send(ctx, hook.URL, event.ExternalID, event.Payload, secrets)
	var status enums.DeliveryStatus
	err = database.Transact(ctx, a.DB, func(tx database.Database) error {
		if _, err := webhooks.FinishAttempt(ctx, tx, input.OrganizationID, delivery.ID, attempt.ID, result.StatusCode, result.ErrorCode); err != nil {
			return err
		}
		if result.StatusCode >= 200 && result.StatusCode < 300 && result.ErrorCode == "" {
			if _, err := webhooks.CompleteDelivery(ctx, tx, input.OrganizationID, delivery.ID, enums.DeliveryStatusSucceeded); err != nil {
				return err
			}
		}
		var err error
		status, err = currentStatus(ctx, tx, input)
		return err
	})
	if err != nil {
		return "", unavailable()
	}
	return status, nil
}

func (a Activities) Complete(ctx context.Context, completion Completion) (enums.DeliveryStatus, error) {
	input := completion.Input
	if err := input.normalize(); err != nil {
		return "", err
	}
	if !completion.Status.IsTerminal() {
		return "", temporal.NewNonRetryableApplicationError("invalid webhook completion status", "InvalidWebhookDelivery", nil) //nolint:wrapcheck // Preserve Temporal's nonretryable classification.
	}
	delivery, err := webhooks.GetDelivery(ctx, a.DB, input.OrganizationID, input.DeliveryID)
	if err != nil {
		return "", unavailable()
	}
	if delivery != nil {
		if _, err := webhooks.CompleteDelivery(ctx, a.DB, input.OrganizationID, delivery.ID, completion.Status); err != nil {
			return "", unavailable()
		}
	}
	return currentStatus(ctx, a.DB, input)
}

func currentStatus(ctx context.Context, db database.Database, input Input) (enums.DeliveryStatus, error) {
	delivery, err := webhooks.GetDelivery(ctx, db, input.OrganizationID, input.DeliveryID)
	if err != nil {
		return "", unavailable()
	}
	if delivery != nil {
		return delivery.Status, nil
	}
	if _, err := webhooks.CancelUnavailable(ctx, db, input.OrganizationID, input.DeliveryID); err != nil {
		return "", unavailable()
	}
	return enums.DeliveryStatusCanceled, nil
}

func unavailable() error {
	return temporal.NewApplicationError("webhook delivery persistence or configuration unavailable", "WebhookUnavailable") //nolint:wrapcheck // Never serialize database, destination, payload, or encryption errors.
}
