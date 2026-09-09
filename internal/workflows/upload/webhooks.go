package upload

import (
	"context"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/database"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/webhookevents"
	"nautilus/internal/database/webhooks"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
)

func (a Activities) publish(ctx context.Context, input Input, key string, size int64) error {
	err := database.Transact(ctx, a.DB, func(tx database.Database) error {
		doc, err := documents.GetByExternalID(ctx, tx, input.OrganizationID, input.DocumentID)
		if err != nil {
			return err
		}
		if doc != nil && doc.Status == enums.DocumentStatusUploading {
			if doc.PageCount > 0 {
				doc, err = documents.PublishPDF(ctx, tx, input.OrganizationID, input.DocumentID, key, size)
			} else {
				doc, err = documents.MarkUploaded(ctx, tx, input.OrganizationID, input.DocumentID)
			}
			if err != nil {
				return err
			}
			if doc == nil {
				doc, err = documents.GetByExternalID(ctx, tx, input.OrganizationID, input.DocumentID)
				if err != nil {
					return err
				}
			}
		}
		if doc == nil || doc.Status != enums.DocumentStatusUploaded {
			return uploadFailure("document upload unavailable", true)
		}
		event, created, err := webhookevents.Record(ctx, tx, input.OrganizationID, &webhookevents.CreateOptions{
			Type: enums.WebhookEventTypeDocumentAvailable, IdempotencyKey: "document.available:" + input.DocumentID,
			OccurredAt: doc.UpdatedAt, DocumentID: input.DocumentID,
		})
		if err != nil {
			return err
		}
		if event == nil {
			return uploadFailure("document upload unavailable", true)
		}
		if created {
			_, err = webhooks.CreateDeliveries(ctx, tx, input.OrganizationID, event.ID, event.Type)
		}
		return err
	})
	if err != nil {
		var failure *temporal.ApplicationError
		if errors.As(err, &failure) {
			return failure
		}
		return uploadFailure("unable to publish document availability", false)
	}
	return nil
}

func (a Activities) WebhookDeliveries(ctx context.Context, input Input) ([]string, error) {
	if err := input.normalize(); err != nil {
		return nil, err
	}
	// Publication may commit immediately before organization deletion. Internal
	// handoff still needs those IDs so each child can durably cancel its delivery.
	ids, err := a.persistedDeliveries(ctx, input)
	if err != nil || len(ids) > 0 {
		return ids, err
	}
	event, err := webhookevents.GetByKey(ctx, a.DB, input.OrganizationID, "document.available:"+input.DocumentID)
	if err != nil {
		return nil, uploadFailure("unable to read document event", false)
	}
	// A resumed pre-webhook workflow may already have recorded Finalize's success.
	if event == nil {
		doc, err := documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
		if err != nil {
			return nil, uploadFailure("unable to read document", false)
		}
		if doc == nil || doc.Status != enums.DocumentStatusUploaded {
			return nil, uploadFailure("document upload unavailable", true)
		}
		if err := a.publish(ctx, input, "", 0); err != nil {
			return nil, err
		}
		event, err = webhookevents.GetByKey(ctx, a.DB, input.OrganizationID, "document.available:"+input.DocumentID)
		if err != nil || event == nil {
			return nil, uploadFailure("unable to read document event", false)
		}
	}
	return a.persistedDeliveries(ctx, input)
}

func (a Activities) persistedDeliveries(ctx context.Context, input Input) ([]string, error) {
	rows, err := a.DB.Query(ctx, `SELECT d.external_id FROM webhook_deliveries d
		JOIN events e ON e.organization_id = d.organization_id AND e.id = d.event_id
		WHERE d.organization_id = $1 AND e.idempotency_key = $2 AND d.trigger = $3
		ORDER BY d.id`, input.OrganizationID, "document.available:"+input.DocumentID, enums.DeliveryTriggerEvent)
	if err != nil {
		return nil, uploadFailure("unable to read document deliveries", false)
	}
	ids := []string{}
	err = database.ScanRows(rows, func(row database.Row) error {
		var id string
		if err := row.Scan(&id); err != nil {
			return uploadFailure("unable to read document delivery", false)
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, uploadFailure("unable to read document deliveries", false)
	}
	return ids, nil
}
