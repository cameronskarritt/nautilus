package auditlogs

import (
	"context"
	"encoding/json"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
)

func Create(
	ctx context.Context,
	db database.Database,
	actorID int,
	auditType enums.AuditType,
	targetOrgID optional.Optional[int],
	payload optional.Optional[any],
) (*AuditLog, error) {
	var payloadJSON []byte
	if payload.Set {
		var err error
		payloadJSON, err = json.Marshal(payload.Data)
		if err != nil {
			return nil, errors.Wrap(err, "unable to marshal audit log payload")
		}
	}

	query := `
		INSERT INTO audit_logs(actor_id, type, target_org_id, payload)
		VALUES ($1, $2, $3, $4)
		RETURNING id, external_id, created_at;
	`

	var id int
	var externalID string
	var createdAt time.Time
	err := db.QueryRow(ctx, query, actorID, auditType, targetOrgID, payloadJSON).Scan(&id, &externalID, &createdAt)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create audit log")
	}

	log := &AuditLog{
		ID:          id,
		ExternalID:  externalID,
		ActorID:     actorID,
		Type:        auditType,
		TargetOrgID: targetOrgID,
		Payload:     payload,
		CreatedAt:   createdAt,
	}

	return log, nil
}
