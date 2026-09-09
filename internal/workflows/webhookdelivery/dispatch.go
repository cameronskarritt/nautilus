package webhookdelivery

import (
	"context"
	"time"
	"uuid"

	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/temporal/failure"
)

const testName = "WebhookTest"
const prepareTestName = "PrepareWebhookTest"
const replayName = "WebhookReplay"
const prepareReplayName = "PrepareWebhookReplay"

type TestInput struct {
	OrganizationID int    `json:"organization_id"`
	WebhookID      string `json:"webhook_id"`
	DeliveryID     string `json:"delivery_id"`
}

func (i *TestInput) normalize() error {
	input := Input{OrganizationID: i.OrganizationID, DeliveryID: i.DeliveryID}
	if err := input.normalize(); err != nil {
		return err
	}
	hook, err := uuid.Parse(i.WebhookID)
	if err != nil {
		return invalidDispatch()
	}
	i.DeliveryID, i.WebhookID = input.DeliveryID, hook.String()
	return nil
}

// ReplayInput is internal; replay is deliberately absent from public routes.
type ReplayInput struct {
	OrganizationID int    `json:"organization_id"`
	WebhookID      string `json:"webhook_id"`
	EventID        string `json:"event_id"`
	RequestID      string `json:"request_id"`
	ReplayedID     string `json:"replayed_id,omitempty"`
}

func (i *ReplayInput) normalize() error {
	test := TestInput{OrganizationID: i.OrganizationID, WebhookID: i.WebhookID, DeliveryID: i.RequestID}
	if err := test.normalize(); err != nil {
		return err
	}
	event, err := uuid.Parse(i.EventID)
	if err != nil {
		return invalidDispatch()
	}
	i.WebhookID, i.RequestID, i.EventID = test.WebhookID, test.DeliveryID, event.String()
	if i.ReplayedID != "" {
		id, err := uuid.Parse(i.ReplayedID)
		if err != nil {
			return invalidDispatch()
		}
		i.ReplayedID = id.String()
	}
	return nil
}

func TestWorkflow(ctx workflow.Context, input TestInput) error {
	if err := input.normalize(); err != nil {
		return err
	}
	return dispatch(ctx, prepareTestName, input)
}

func ReplayWorkflow(ctx workflow.Context, input ReplayInput) error {
	if err := input.normalize(); err != nil {
		return err
	}
	return dispatch(ctx, prepareReplayName, input)
}

func dispatch(ctx workflow.Context, name string, input any) error {
	// Once accepted, finish the durable handoff even if the bootstrap is canceled.
	ctx, _ = workflow.NewDisconnectedContext(ctx)
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumInterval: time.Minute},
	})
	var delivery Input
	if err := workflow.ExecuteActivity(ctx, name, input).Get(ctx, &delivery); err != nil {
		return errors.Wrap(err, "prepare webhook dispatch")
	}
	if delivery.DeliveryID == "" {
		return nil
	}
	return StartChild(ctx, delivery)
}

func (a Activities) PrepareTest(ctx context.Context, input TestInput) (Input, error) {
	if err := input.normalize(); err != nil {
		return Input{}, err
	}
	var delivery *webhooks.Delivery
	err := database.Transact(ctx, a.DB, func(tx database.Database) error {
		event, _, err := webhookevents.Record(ctx, tx, input.OrganizationID, &webhookevents.CreateOptions{
			Type: enums.WebhookEventTypeWebhookTest, IdempotencyKey: "webhook.test:" + input.DeliveryID, OccurredAt: time.Now(),
		})
		if err != nil || event == nil {
			return err
		}
		delivery, err = webhooks.CreateTestDelivery(ctx, tx, input.OrganizationID, event.ID, input.WebhookID, input.DeliveryID)
		return err
	})
	if errors.Is(err, webhooks.ErrConflict) || errors.Is(err, webhooks.ErrInvalidOptions) {
		return Input{}, invalidDispatch()
	}
	if err != nil {
		return Input{}, unavailable()
	}
	if delivery == nil {
		return Input{}, nil
	}
	return Input{OrganizationID: input.OrganizationID, DeliveryID: delivery.ExternalID}, nil
}

func (a Activities) PrepareReplay(ctx context.Context, input ReplayInput) (Input, error) {
	if err := input.normalize(); err != nil {
		return Input{}, err
	}
	lineage := optional.Empty[string]()
	if input.ReplayedID != "" {
		lineage = optional.Set(input.ReplayedID)
	}
	delivery, err := webhooks.Replay(ctx, a.DB, input.OrganizationID, input.EventID, input.WebhookID, "replay:"+input.RequestID, lineage)
	if errors.Is(err, webhooks.ErrConflict) || errors.Is(err, webhooks.ErrInvalidOptions) {
		return Input{}, invalidDispatch()
	}
	if err != nil {
		return Input{}, unavailable()
	}
	if delivery == nil {
		return Input{}, nil
	}
	return Input{OrganizationID: input.OrganizationID, DeliveryID: delivery.ExternalID}, nil
}

func invalidDispatch() error {
	return failure.New("invalid webhook dispatch identifiers", "InvalidWebhookDispatch", true)
}
