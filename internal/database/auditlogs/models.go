package auditlogs

import (
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/optional"
)

type AuditLog struct {
	ID          int                    `json:"-"`
	ExternalID  string                 `json:"id"`
	ActorID     int                    `json:"actor_id"`
	Type        enums.AuditType        `json:"type"`
	TargetOrgID optional.Optional[int] `json:"target_org_id,omitzero"`
	Payload     optional.Optional[any] `json:"payload,omitzero"`
	CreatedAt   time.Time              `json:"created_at"`
}
