package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/log"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRequireAdmin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name           string
		SetupUser      func(t *testing.T, db interface{}) *users.User
		ExpectedStatus int
		ExpectedPass   bool
	}{
		{
			Name: "admin user passes through",
			SetupUser: func(t *testing.T, db interface{}) *users.User {
				t.Helper()
				return &users.User{
					ID:    1,
					Admin: true,
				}
			},
			ExpectedStatus: http.StatusOK,
			ExpectedPass:   true,
		},
		{
			Name: "non-admin user is forbidden",
			SetupUser: func(t *testing.T, db interface{}) *users.User {
				t.Helper()
				return &users.User{
					ID:    1,
					Admin: false,
				}
			},
			ExpectedStatus: http.StatusForbidden,
			ExpectedPass:   false,
		},
		{
			Name: "nil user is forbidden",
			SetupUser: func(t *testing.T, db interface{}) *users.User {
				t.Helper()
				return nil
			},
			ExpectedStatus: http.StatusForbidden,
			ExpectedPass:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			handlerCalled := false
			handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				handlerCalled = true
				w.WriteHeader(http.StatusOK)
				w.Write([]byte("success"))
			})

			ctx := log.WithContext(context.Background(), log.Default())
			user := tt.SetupUser(t, nil)
			if user != nil {
				ctx = users.WithContext(ctx, user)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req = req.WithContext(ctx)
			rec := httptest.NewRecorder()

			middleware := RequireAdmin(handler)
			middleware.ServeHTTP(rec, req)

			require.Equal(t, tt.ExpectedStatus, rec.Code)
			require.Equal(t, tt.ExpectedPass, handlerCalled)

			if !tt.ExpectedPass {
				var response map[string]interface{}
				err := json.Unmarshal(rec.Body.Bytes(), &response)
				require.NoError(t, err)
				require.Equal(t, "Access denied", response["message"])
			}
		})
	}
}

func TestRequireAdmin_WithDatabase(t *testing.T) {
	t.Parallel()

	t.Run("admin user from database passes through", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})

		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)
		require.True(t, adminUser.Admin)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := RequireAdmin(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, handlerCalled)
	})

	t.Run("regular user from database is forbidden", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create regular user (not admin)
		regularUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "regular",
		})

		regularUser, err := users.Get(ctx, db, regularUserID)
		require.NoError(t, err)
		require.False(t, regularUser.Admin)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, regularUser)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := RequireAdmin(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusForbidden, rec.Code)
		require.False(t, handlerCalled)
	})
}

func TestAdminOrgOverride(t *testing.T) {
	t.Parallel()

	t.Run("non-admin user with header is ignored", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create regular user
		userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "regular",
		})
		user, err := users.Get(ctx, db, userID)
		require.NoError(t, err)

		// Create org
		org, err := organizations.Create(ctx, db, "test-org", "Test Org", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			// Verify org context is NOT set
			orgFromCtx := organizations.FromContext(r.Context())
			require.Nil(t, orgFromCtx)
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, user)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(orgOverrideHeader, org.Slug)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, handlerCalled)
	})

	t.Run("admin without header passes through unchanged", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})
		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			// Verify org context is NOT set
			orgFromCtx := organizations.FromContext(r.Context())
			require.Nil(t, orgFromCtx)
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		// No header set
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, handlerCalled)
	})

	t.Run("admin with valid slug gets org context set", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})
		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)

		// Create org to override into
		org, err := organizations.Create(ctx, db, "acme-corp", "Acme Corp", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true

			// Verify org context is set
			orgFromCtx := organizations.FromContext(r.Context())
			require.NotNil(t, orgFromCtx)
			require.Equal(t, org.ID, orgFromCtx.ID)
			require.Equal(t, org.Slug, orgFromCtx.Slug)

			// Verify org member context is set with virtual member
			memberFromCtx := organizations.MemberFromContext(r.Context())
			require.NotNil(t, memberFromCtx)
			require.Equal(t, 0, memberFromCtx.ID) // Virtual member has ID 0
			require.Equal(t, adminUser.ID, memberFromCtx.UserID)
			require.Equal(t, org.ID, memberFromCtx.OrganizationID)
			require.Equal(t, organizations.RoleOwner, memberFromCtx.Role)

			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(orgOverrideHeader, org.Slug)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, handlerCalled)
	})

	t.Run("admin with invalid slug returns 404", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})
		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(orgOverrideHeader, "nonexistent-org")
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusNotFound, rec.Code)
		require.False(t, handlerCalled)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Organization not found", response["message"])
	})

	t.Run("admin override replaces existing org context", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})
		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)

		// Create original org (the one in session context)
		originalOrg, err := organizations.Create(ctx, db, "original-org", "Original Org", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		// Create target org (the one to override to)
		targetOrg, err := organizations.Create(ctx, db, "target-org", "Target Org", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true

			// Verify org context is the target org, not original
			orgFromCtx := organizations.FromContext(r.Context())
			require.NotNil(t, orgFromCtx)
			require.Equal(t, targetOrg.ID, orgFromCtx.ID)
			require.Equal(t, targetOrg.Slug, orgFromCtx.Slug)

			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		// Set original org in context (simulating what RequireSession would do)
		ctx = organizations.WithContext(ctx, originalOrg)

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(orgOverrideHeader, targetOrg.Slug)
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, handlerCalled)
	})

	t.Run("admin with mismatched header returns conflict error", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		// Create admin user
		adminUserID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{
			Suffix: "admin",
			Admin:  true,
		})
		adminUser, err := users.Get(ctx, db, adminUserID)
		require.NoError(t, err)

		// Create org that's in the session (assumed org)
		sessionOrg, err := organizations.Create(ctx, db, "session-org", "Session Org", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		// Create different org that's in the header
		headerOrg, err := organizations.Create(ctx, db, "header-org", "Header Org", false, optional.Empty[organizations.Settings]())
		require.NoError(t, err)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		// Set assumed org ID in context (simulating what RequireSession would do)
		ctx = sessions.WithAssumedOrgID(ctx, optional.Set(sessionOrg.ID))

		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set(orgOverrideHeader, headerOrg.Slug) // Different org in header
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()

		middleware := AdminOrgOverride(db)(handler)
		middleware.ServeHTTP(rec, req)

		require.Equal(t, http.StatusConflict, rec.Code)
		require.False(t, handlerCalled)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Assumed organization mismatch", response["message"])

		// Verify error code is present
		errors, ok := response["errors"].([]interface{})
		require.True(t, ok)
		require.Len(t, errors, 1)
		errorDetail, ok := errors[0].(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, "ADMIN-02", errorDetail["code"])
	})
}
