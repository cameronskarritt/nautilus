package auditlogs_test

import (
	"context"
	"encoding/json"
	"testing"

	"nautilus/internal/database/auditlogs"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		auditType     enums.AuditType
		targetOrgSlug string
		targetOrgName string
		payload       optional.Optional[any]
	}{
		{
			name:          "with target org",
			auditType:     enums.AuditTypeOrgAssume,
			targetOrgSlug: "audit-target-org",
			targetOrgName: "Audit Target Org",
			payload:       optional.Empty[any](),
		},
		{
			name:      "without target org",
			auditType: enums.AuditTypeOrgUnassume,
			payload:   optional.Empty[any](),
		},
		{
			name:          "with payload",
			auditType:     enums.AuditTypeOrgAssume,
			targetOrgSlug: "audit-payload-org",
			targetOrgName: "Audit Payload Org",
			payload: optional.Set[any](map[string]string{
				"org_slug": "audit-payload-org",
				"org_name": "Audit Payload Org",
			}),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := context.Background()

			userID := testutil.CreateTestUser(t, db, nil)
			targetOrgID := optional.Empty[int]()
			if tt.targetOrgSlug != "" {
				targetOrgID = optional.Set(testutil.CreateTestOrg(t, db, tt.targetOrgSlug, tt.targetOrgName))
			}

			log, err := auditlogs.Create(ctx, db, userID, tt.auditType, targetOrgID, tt.payload)

			require.NoError(t, err)
			require.NotNil(t, log)
			require.NotZero(t, log.ID)
			require.NotEmpty(t, log.ExternalID)
			require.Equal(t, userID, log.ActorID)
			require.Equal(t, tt.auditType, log.Type)
			require.Equal(t, targetOrgID.Set, log.TargetOrgID.Set)
			if targetOrgID.Set {
				require.Equal(t, targetOrgID.Data, log.TargetOrgID.Data)
			}
			require.Equal(t, tt.payload.Set, log.Payload.Set)
			if tt.payload.Set {
				require.Equal(t, tt.payload.Data, log.Payload.Data)
			}
			require.False(t, log.CreatedAt.IsZero())

			var persistedActorID int
			var persistedType string
			var persistedTargetOrgID optional.Optional[int]
			var persistedPayload optional.Optional[string]
			err = db.QueryRow(ctx, `
				SELECT actor_id, type, target_org_id, payload::text
				FROM audit_logs
				WHERE id = $1;
			`, log.ID).Scan(&persistedActorID, &persistedType, &persistedTargetOrgID, &persistedPayload)
			require.NoError(t, err)

			require.Equal(t, userID, persistedActorID)
			require.Equal(t, tt.auditType.String(), persistedType)
			require.Equal(t, targetOrgID.Set, persistedTargetOrgID.Set)
			if targetOrgID.Set {
				require.Equal(t, targetOrgID.Data, persistedTargetOrgID.Data)
			}
			require.Equal(t, tt.payload.Set, persistedPayload.Set)
			if tt.payload.Set {
				expectedPayload, err := json.Marshal(tt.payload.Data)
				require.NoError(t, err)
				require.JSONEq(t, string(expectedPayload), persistedPayload.Data)
			}
		})
	}
}
