package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/log"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestGetUserExternal(t *testing.T) {
	db := testutil.SetupTestDB(t)
	mux := NewMux(db, nil, testutil.NewTestFeatureFlagger())
	user := createUser(t, db, "testuser")

	tests := []struct {
		Name       string
		Path       string
		WantStatus int
	}{
		{
			Name:       "get user by id",
			Path:       "/?id=" + user.ExternalID,
			WantStatus: http.StatusOK,
		},
		{
			Name:       "get user by username",
			Path:       "/?username=testuser",
			WantStatus: http.StatusOK,
		},
		{
			Name:       "missing id and username returns error",
			Path:       "/",
			WantStatus: http.StatusBadRequest,
		},
		{
			Name:       "invalid uuid returns not found",
			Path:       "/?id=invalid-uuid",
			WantStatus: http.StatusNotFound,
		},
		{
			Name:       "valid uuid but user not found returns not found",
			Path:       "/?id=00000000-0000-0000-0000-000000000000",
			WantStatus: http.StatusNotFound,
		},
		{
			Name:       "username not found returns not found",
			Path:       "/?username=nonexistent",
			WantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tt.Path, nil)
			rec := httptest.NewRecorder()

			mux.GetUserExternal(rec, req)

			require.Equal(t, tt.WantStatus, rec.Code)
			if tt.WantStatus != http.StatusOK {
				return
			}

			var response map[string]users.UserExternal
			err := json.Unmarshal(rec.Body.Bytes(), &response)
			require.NoError(t, err)
			require.Equal(t, user.ExternalID, response["user"].ExternalID)
			require.Equal(t, user.Username.Data, response["user"].Username)
		})
	}
}

func TestMe(t *testing.T) {
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	type meResponse struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
		Organization *struct {
			Slug string `json:"slug"`
		} `json:"organization"`
		Assumed bool            `json:"assumed"`
		Flags   map[string]bool `json:"flags"`
	}

	t.Run("returns normal organization as not assumed", func(t *testing.T) {
		user := createUser(t, db, "meuser")
		orgID := testutil.CreateTestOrg(t, db, "me-org", "Me Org")
		memberID := testutil.CreateTestOrgMember(t, db, user.ID, orgID, organizations.RoleOwner)

		session, err := sessions.Create(ctx, db, user.ID, optional.Set(memberID), nil)
		require.NoError(t, err)

		mux := NewMux(db, nil, testutil.NewTestFeatureFlagger())
		handler := middleware.RequireSession(db)(middleware.AdminOrgOverride(db)(http.HandlerFunc(mux.Me)))

		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response meResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, user.ExternalID, response.User.ID)
		require.NotNil(t, response.Organization)
		require.Equal(t, "me-org", response.Organization.Slug)
		require.False(t, response.Assumed)
		require.NotNil(t, response.Flags)
	})

	t.Run("returns session override organization as assumed", func(t *testing.T) {
		adminID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "admin", Admin: true})
		adminUser, err := users.Get(ctx, db, adminID)
		require.NoError(t, err)

		orgID := testutil.CreateTestOrg(t, db, "assumed-me-org", "Assumed Me Org")
		org, err := organizations.Get(ctx, db, orgID)
		require.NoError(t, err)

		session, err := sessions.Create(ctx, db, adminUser.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		sessionFromDB, err := sessions.Get(ctx, db, session.Token)
		require.NoError(t, err)

		err = sessions.AssumeOrg(ctx, db, sessionFromDB.ID, org.ID)
		require.NoError(t, err)

		mux := NewMux(db, nil, testutil.NewTestFeatureFlagger())
		handler := middleware.RequireSession(db)(middleware.AdminOrgOverride(db)(http.HandlerFunc(mux.Me)))

		req := httptest.NewRequest(http.MethodGet, "/me", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response meResponse
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, adminUser.ExternalID, response.User.ID)
		require.NotNil(t, response.Organization)
		require.Equal(t, org.Slug, response.Organization.Slug)
		require.True(t, response.Assumed)
		require.NotNil(t, response.Flags)
	})
}

func TestUpdateUser(t *testing.T) {
	db := testutil.SetupTestDB(t)
	mux := NewMux(db, nil, testutil.NewTestFeatureFlagger())

	t.Run("updates username", func(t *testing.T) {
		user := createUser(t, db, "updateuser")
		req := httptest.NewRequest(http.MethodPatch, "/me", strings.NewReader(`{"username":" renamed "}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(users.WithContext(req.Context(), user))
		rec := httptest.NewRecorder()

		mux.UpdateUser(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]users.User
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, user.ExternalID, response["user"].ExternalID)
		require.Equal(t, "renamed", response["user"].Username.Data)
	})

	t.Run("rejects duplicate username", func(t *testing.T) {
		createUser(t, db, "takenuser")
		user := createUser(t, db, "availableuser")

		req := httptest.NewRequest(http.MethodPatch, "/me", strings.NewReader(`{"username":"takenuser"}`))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(users.WithContext(req.Context(), user))
		rec := httptest.NewRecorder()

		mux.UpdateUser(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func createUser(t *testing.T, db database.Database, username string) *users.User {
	t.Helper()

	user, err := users.Register(context.Background(), db,
		optional.Set(username),
		optional.Set(username+"@example.com"),
		"password123",
	)
	require.NoError(t, err)

	return user
}
