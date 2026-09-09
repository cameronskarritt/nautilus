package webhooks_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"uuid"

	"nautilus/internal/enums"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/mocks"

	"nautilus/internal/app/handlers/webhooks"
	"nautilus/internal/database/organizations"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/webhookdelivery"
)

func TestWebhookTestStartsWorkflowBeforePersistence(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		startErr error
		status   int
	}{
		{"accepted", nil, http.StatusAccepted},
		{"start failure", context.DeadlineExceeded, http.StatusInternalServerError},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			workflows := mocks.NewClient(t)
			router := mux.New(mux.Config{})
			webhooks.NewMux(db, workflows).Mount(router, "/webhooks")
			id, _ := create(t, router, ctx)
			var started webhookdelivery.TestInput
			workflows.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Run(func(args mock.Arguments) {
				started = args.Get(3).(webhookdelivery.TestInput)
				require.Equal(t, org.ID, started.OrganizationID)
				require.Equal(t, id, started.WebhookID)
				_, err := uuid.Parse(started.DeliveryID)
				require.NoError(t, err)
				var count int
				require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE organization_id=$1`, org.ID).Scan(&count))
				require.Zero(t, count)
				require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM webhook_events WHERE organization_id=$1`, org.ID).Scan(&count))
				require.Zero(t, count)
			}).Return(nil, tt.startErr).Once()
			rec := request(router, ctx, http.MethodPost, "/webhooks/"+id+"/test", "")
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if tt.startErr == nil {
				var response struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
				require.Equal(t, started.DeliveryID, response.ID)
				require.Equal(t, "pending", response.Status)
			} else {
				require.Contains(t, rec.Body.String(), "HTTP-500")
				require.NotContains(t, rec.Body.String(), tt.startErr.Error())
			}
			var count int
			require.NoError(t, db.QueryRow(ctx, `SELECT count(*) FROM webhook_deliveries WHERE organization_id=$1`, org.ID).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestWebhookTestRejectsUnavailableEndpoint(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		disabled bool
		role     enums.Role
		foreign  bool
		status   int
		code     string
	}{
		{name: "disabled", disabled: true, role: enums.RoleOwner, status: 409, code: "WEBHOOK-11"},
		{name: "member", role: enums.RoleMember, status: 403, code: "WEBHOOK-02"},
		{name: "viewer", role: enums.RoleViewer, status: 403, code: "WEBHOOK-02"},
		{name: "foreign organization", role: enums.RoleOwner, foreign: true, status: 404, code: "HTTP-404"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			workflows := mocks.NewClient(t)
			router := mux.New(mux.Config{})
			webhooks.NewMux(db, workflows).Mount(router, "/webhooks")
			id, _ := create(t, router, ctx)
			if tt.disabled {
				_, err := db.Exec(ctx, `UPDATE webhooks SET enabled=false WHERE external_id=$1`, id)
				require.NoError(t, err)
			}
			if tt.foreign {
				otherID := testutil.CreateTestOrg(t, db, "other-test", "Other")
				other, err := organizations.Get(ctx, db, otherID)
				require.NoError(t, err)
				ctx = organizations.WithContext(ctx, other)
				org = other
			}
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: tt.role})
			rec := request(router, ctx, http.MethodPost, "/webhooks/"+id+"/test", "")
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.code)
		})
	}
}
