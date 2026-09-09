package webhooks_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"uuid"

	"nautilus/internal/app/handlers/webhooks"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type keys struct{}

func (keys) UserKey(context.Context) ([]byte, error) { return bytes.Repeat([]byte{1}, 32), nil }
func (keys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{2}, 32), nil
}
func actor(t *testing.T, db database.Database) (context.Context, *organizations.Organization) {
	t.Helper()
	orgID := testutil.CreateTestOrg(t, db, "hooks", "Hooks")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	ctx := organizations.WithContext(t.Context(), org)
	ctx = users.WithContext(ctx, &users.User{ID: 1})
	ctx = sessions.WithContext(ctx, 1)
	ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: orgID, Role: organizations.RoleOwner})
	return encrypt.WithContext(ctx, encrypt.ForOrganization(keys{}, org.ExternalID)), org
}
func request(router http.Handler, ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, r)
	return w
}

const createBody = `{"name":" Orders ","url":"https://example.com/hook","event_types":["document.available","document.available"]}`

func create(t *testing.T, r http.Handler, ctx context.Context) (string, string) {
	t.Helper()
	rec := request(r, ctx, http.MethodPost, "/webhooks", createBody)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
	var response struct {
		Webhook struct {
			ID         string   `json:"id"`
			Name       string   `json:"name"`
			EventTypes []string `json:"event_types"`
		} `json:"webhook"`
		Secret string `json:"secret"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "Orders", response.Webhook.Name)
	require.Equal(t, []string{"document.available"}, response.Webhook.EventTypes)
	require.True(t, strings.HasPrefix(response.Secret, "whsec_"))
	return response.Webhook.ID, response.Secret
}
func TestWebhookCRUDAndRotation(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx, org := actor(t, db)
	router := mux.New(mux.Config{})
	webhooks.NewMux(db, nil).Mount(router, "/webhooks")
	id, secret := create(t, router, ctx)
	var ciphertext []byte
	require.NoError(t, db.QueryRow(ctx, `SELECT signing_secret FROM webhooks WHERE external_id=$1`, id).Scan(&ciphertext))
	raw, err := encrypt.FromContext(ctx).Open(ctx, ciphertext, encrypt.Binding{Purpose: "webhook-signing-secret", RecordID: id})
	require.NoError(t, err)
	require.Len(t, raw, 32)
	require.Equal(t, secret, "whsec_"+base64.StdEncoding.EncodeToString(raw))
	for _, path := range []string{"/webhooks", "/webhooks/" + id} {
		rec := request(router, ctx, http.MethodGet, path, "")
		require.Equal(t, http.StatusOK, rec.Code)
		require.NotContains(t, rec.Body.String(), "secret")
		require.NotContains(t, rec.Body.String(), base64.StdEncoding.EncodeToString(ciphertext))
	}
	rec := request(router, ctx, http.MethodPatch, "/webhooks/"+id, `{"enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Contains(t, rec.Body.String(), `"enabled":false`)
	require.Contains(t, rec.Body.String(), `"name":"Orders"`)
	rec = request(router, ctx, http.MethodPost, "/webhooks/"+id+"/secret/rotate", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.NotContains(t, rec.Body.String(), secret)
	var previous []byte
	var expires time.Time
	require.NoError(t, db.QueryRow(ctx, `SELECT previous_signing_secret,previous_secret_expires_at FROM webhooks WHERE external_id=$1`, id).Scan(&previous, &expires))
	require.Equal(t, ciphertext, previous)
	require.WithinDuration(t, time.Now().Add(24*time.Hour), expires, time.Minute)
	rec = request(router, ctx, http.MethodPost, "/webhooks/"+id+"/secret/rotate", "")
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "WEBHOOK-10")
	otherID := testutil.CreateTestOrg(t, db, "other", "Other")
	other, err := organizations.Get(ctx, db, otherID)
	require.NoError(t, err)
	foreign := organizations.WithContext(ctx, other)
	foreign = organizations.WithMemberContext(foreign, &organizations.Member{ID: 2, UserID: 1, OrganizationID: other.ID, Role: organizations.RoleOwner})
	for _, method := range []string{http.MethodGet, http.MethodPatch, http.MethodDelete} {
		rec = request(router, foreign, method, "/webhooks/"+id, `{"enabled":true}`)
		require.Equal(t, http.StatusNotFound, rec.Code)
	}
	rec = request(router, ctx, http.MethodDelete, "/webhooks/"+id, "")
	require.Equal(t, http.StatusNoContent, rec.Code)
	rec = request(router, ctx, http.MethodGet, "/webhooks/"+id, "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	require.Positive(t, org.ID)
}
func TestWebhookAuthorization(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		role     organizations.Role
		memberID int
		session  int
		key      *apikeys.Key
		method   string
		want     int
	}{
		{"owner", organizations.RoleOwner, 1, 1, nil, http.MethodGet, 200},
		{"admin", organizations.RoleAdmin, 1, 1, nil, http.MethodGet, 200},
		{"member", organizations.RoleMember, 1, 1, nil, http.MethodGet, 403},
		{"viewer", organizations.RoleViewer, 1, 1, nil, http.MethodGet, 403},
		{"virtual owner", organizations.RoleOwner, 0, 1, nil, http.MethodGet, 403},
		{"no session", organizations.RoleOwner, 1, 0, nil, http.MethodGet, 403},
		{"read key", "", 0, 0, &apikeys.Key{ID: 1, Scopes: []apikeys.Scope{apikeys.ScopeRead}}, http.MethodGet, 200},
		{"write key cannot read", "", 0, 0, &apikeys.Key{ID: 1, Scopes: []apikeys.Scope{apikeys.ScopeWrite}}, http.MethodGet, 403},
		{"read key cannot create", "", 0, 0, &apikeys.Key{ID: 1, Scopes: []apikeys.Scope{apikeys.ScopeRead}}, http.MethodPost, 403},
		{"write key", "", 0, 0, &apikeys.Key{ID: 1, Scopes: []apikeys.Scope{apikeys.ScopeWrite}}, http.MethodPost, 201},
		{"unpersisted key", "", 0, 0, &apikeys.Key{Scopes: []apikeys.Scope{apikeys.ScopeRead}}, http.MethodGet, 403},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			ctx = sessions.WithContext(ctx, tt.session)
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: tt.memberID, UserID: 1, OrganizationID: org.ID, Role: tt.role})
			if tt.key != nil {
				key := *tt.key
				key.OrganizationID = org.ID
				ctx = apikeys.WithContext(ctx, &key)
			}
			router := mux.New(mux.Config{})
			webhooks.NewMux(db, nil).Mount(router, "/webhooks")
			rec := request(router, ctx, tt.method, "/webhooks", createBody)
			require.Equal(t, tt.want, rec.Code, rec.Body.String())
			if tt.want == 403 {
				require.Contains(t, rec.Body.String(), "WEBHOOK-02")
			}
		})
	}
}
func TestWebhookFormsAndRoutes(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx, _ := actor(t, db)
	router := mux.New(mux.Config{})
	webhooks.NewMux(db, nil).Mount(router, "/webhooks")
	id, _ := create(t, router, ctx)
	for _, tt := range []struct {
		name, method, path, body, code string
		status                         int
	}{
		{"null enabled", "PATCH", "/webhooks/" + id, `{"enabled":null}`, "WEBHOOK-07", 400},
		{"empty patch", "PATCH", "/webhooks/" + id, `{}`, "WEBHOOK-06", 400},
		{"null name", "PATCH", "/webhooks/" + id, `{"name":null}`, "WEBHOOK-03", 400},
		{"null url", "PATCH", "/webhooks/" + id, `{"url":null}`, "WEBHOOK-04", 400},
		{"null types", "PATCH", "/webhooks/" + id, `{"event_types":null}`, "WEBHOOK-05", 400},
		{"invalid URL", "PATCH", "/webhooks/" + id, `{"url":"http://127.0.0.1/private"}`, "WEBHOOK-04", 400},
		{"unknown type", "PATCH", "/webhooks/" + id, `{"event_types":["anything"]}`, "WEBHOOK-05", 400},
		{"malformed UUID", "GET", "/webhooks/invalid", "", "", 404},
		{"no replay", "POST", "/webhooks/" + id + "/replay", "", "", 404},
		{"test unavailable", "POST", "/webhooks/" + id + "/test", "", "WEBHOOK-12", 503},
		{"invalid cursor", "GET", "/webhooks?cursor=invalid", "", "WEBHOOK-08", 400},
		{"method", "PUT", "/webhooks/" + id, "", "", 405},
	} {
		t.Run(tt.name, func(t *testing.T) {
			rec := request(router, ctx, tt.method, tt.path, tt.body)
			require.Equal(t, tt.status, rec.Code, rec.Body.String())
			if tt.code != "" {
				require.Contains(t, rec.Body.String(), tt.code)
				fields := map[string]string{"WEBHOOK-03": "name", "WEBHOOK-04": "url", "WEBHOOK-05": "event_types", "WEBHOOK-07": "enabled", "WEBHOOK-08": "cursor"}
				if field := fields[tt.code]; field != "" {
					require.Contains(t, rec.Body.String(), `"field":"`+field+`"`)
				}
			}
		})
	}
}
func TestWebhookHistoryScope(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx, org := actor(t, db)
	router := mux.New(mux.Config{})
	webhooks.NewMux(db, nil).Mount(router, "/webhooks")
	id, _ := create(t, router, ctx)
	other, _ := create(t, router, ctx)
	eventID := uuid.NewV4().String()
	deliveryID := uuid.NewV4().String()
	_, err := db.Exec(ctx, `INSERT INTO events(external_id,organization_id,type,schema_version,idempotency_key,payload,occurred_at) VALUES($1,$2,'webhook.test',1,'test','{"safe":true}',CURRENT_TIMESTAMP)`, eventID, org.ID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO webhook_deliveries(external_id,organization_id,webhook_id,event_id,trigger,request_key) SELECT $1,$2,w.id,e.id,'event','test' FROM webhooks w,events e WHERE w.external_id=$3 AND e.external_id=$4`, deliveryID, org.ID, id, eventID)
	require.NoError(t, err)
	_, err = db.Exec(ctx, `INSERT INTO webhook_attempts(organization_id,delivery_id,attempt_key,url) SELECT $1,id,'first','https://example.com/hook' FROM webhook_deliveries WHERE external_id=$2`, org.ID, deliveryID)
	require.NoError(t, err)
	path := "/webhooks/" + id + "/deliveries/" + deliveryID
	for _, suffix := range []string{"", "/attempts"} {
		rec := request(router, ctx, "GET", path+suffix, "")
		require.Equal(t, 200, rec.Code, rec.Body.String())
		rec = request(router, ctx, "GET", "/webhooks/"+other+"/deliveries/"+deliveryID+suffix, "")
		require.Equal(t, 404, rec.Code)
	}
	rec := request(router, ctx, "GET", path, "")
	require.Contains(t, rec.Body.String(), `"payload":{"safe":true}`)
	rec = request(router, ctx, "GET", "/webhooks/"+id+"/deliveries", "")
	require.Contains(t, rec.Body.String(), deliveryID)
}

func TestWebhookRejectsInconsistentContexts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		change func(context.Context, *organizations.Organization) context.Context
		method string
		code   string
	}{
		{"missing organization", func(ctx context.Context, _ *organizations.Organization) context.Context {
			return organizations.WithContext(ctx, nil)
		}, "GET", "WEBHOOK-01"},
		{"wrong member organization", func(ctx context.Context, org *organizations.Organization) context.Context {
			return organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID + 1, Role: organizations.RoleOwner})
		}, "GET", "WEBHOOK-02"},
		{"wrong member user", func(ctx context.Context, org *organizations.Organization) context.Context {
			return organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 2, OrganizationID: org.ID, Role: organizations.RoleOwner})
		}, "GET", "WEBHOOK-02"},
		{"missing user", func(ctx context.Context, _ *organizations.Organization) context.Context {
			return users.WithContext(ctx, nil)
		}, "GET", "WEBHOOK-02"},
		{"wrong key organization", func(ctx context.Context, org *organizations.Organization) context.Context {
			return apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID + 1, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
		}, "GET", "WEBHOOK-02"},
		{"missing encryption", func(ctx context.Context, _ *organizations.Organization) context.Context {
			return encrypt.WithContext(ctx, nil)
		}, "POST", "WEBHOOK-02"},
		{"user encryption", func(ctx context.Context, _ *organizations.Organization) context.Context {
			return encrypt.WithContext(ctx, encrypt.ForUser(keys{}))
		}, "POST", "WEBHOOK-02"},
		{"wrong organization encryption", func(ctx context.Context, _ *organizations.Organization) context.Context {
			return encrypt.WithContext(ctx, encrypt.ForOrganization(keys{}, uuid.NewV4().String()))
		}, "POST", "WEBHOOK-02"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			router := mux.New(mux.Config{})
			webhooks.NewMux(db, nil).Mount(router, "/webhooks")
			rec := request(router, tt.change(ctx, org), tt.method, "/webhooks", createBody)
			require.Equal(t, http.StatusForbidden, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
		})
	}
}
