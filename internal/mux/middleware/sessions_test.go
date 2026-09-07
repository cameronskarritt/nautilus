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
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRequireSessionRejectsAnotherUsersMembership(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	otherID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Suffix: "other"})
	orgID := testutil.CreateTestOrg(t, db, t.Name(), "Organization")
	memberID := testutil.CreateTestOrgMember(t, db, otherID, orgID, organizations.RoleOwner)
	session, err := sessions.Create(t.Context(), db, userID, optional.Set(memberID), nil)
	require.NoError(t, err)

	router := mux.New()
	router.Use(RequireSession(db))
	router.Get("/protected", func(http.ResponseWriter, *http.Request) {
		t.Fatal("session with another user's membership reached handler")
	})
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.AddCookie(sessions.CreateCookie(session.Token))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.JSONEq(t, `{"message":"Unable to process request","errors":[{"message":"user has no session","code":"SESS-01"}]}`, rec.Body.String())
}

func TestRequireSession(t *testing.T) {
	t.Parallel()

	t.Run("valid session with user passes through", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, nil)
		session, err := sessions.Create(ctx, db, userID, optional.Empty[int](), nil)
		require.NoError(t, err)

		handlerCalled := false
		var capturedUser *users.User
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedUser = users.FromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, capturedUser)
		require.Equal(t, userID, capturedUser.ID)
	})

	t.Run("missing session cookie returns 401", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.False(t, handlerCalled)
		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err := json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to process request", response["message"])
		// Check that errors array contains the detail
		errors, ok := response["errors"].([]any)
		require.True(t, ok)
		require.Greater(t, len(errors), 0)
		errorDetail, ok := errors[0].(map[string]any)
		require.True(t, ok)
		require.Equal(t, "user has no session", errorDetail["message"])
	})

	t.Run("invalid session token returns error", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)

		handlerCalled := false
		handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			handlerCalled = true
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie("invalid-token"))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.False(t, handlerCalled)
		require.NotEqual(t, http.StatusOK, rec.Code)
	})

	t.Run("session with organization context", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, nil)
		orgID := testutil.CreateTestOrg(t, db, "test-org", "Test Org")
		orgMemberID := testutil.CreateTestOrgMember(t, db, userID, orgID, organizations.RoleOwner)

		org, err := organizations.Get(ctx, db, orgID)
		require.NoError(t, err)
		orgMember, err := organizations.GetMember(ctx, db, orgMemberID)
		require.NoError(t, err)

		session, err := sessions.Create(ctx, db, userID, optional.Set(orgMember.ID), nil)
		require.NoError(t, err)

		handlerCalled := false
		var capturedUser *users.User
		var capturedOrg *organizations.Organization
		var capturedMember *organizations.Member
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedUser = users.FromContext(r.Context())
			capturedOrg = organizations.FromContext(r.Context())
			capturedMember = organizations.MemberFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.NotNil(t, capturedUser)
		require.NotNil(t, capturedOrg)
		require.NotNil(t, capturedMember)
		require.Equal(t, org.ID, capturedOrg.ID)
		require.Equal(t, orgMember.ID, capturedMember.ID)
	})

	t.Run("session with assumed org sets context", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Admin: true})
		orgID := testutil.CreateTestOrg(t, db, "assumed-test-org", "Assumed Test Org")

		// Create session and assume org
		session, err := sessions.Create(ctx, db, userID, optional.Empty[int](), nil)
		require.NoError(t, err)

		sessionFromDB, err := sessions.Get(ctx, db, session.Token)
		require.NoError(t, err)

		err = sessions.AssumeOrg(ctx, db, sessionFromDB.ID, orgID)
		require.NoError(t, err)

		handlerCalled := false
		var capturedAssumedOrgID optional.Optional[int]
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedAssumedOrgID = sessions.AssumedOrgIDFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.True(t, capturedAssumedOrgID.Set)
		require.Equal(t, orgID, capturedAssumedOrgID.Data)
	})

	t.Run("session with non-existent org member", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, nil)
		// Create a valid org and org member first
		orgID := testutil.CreateTestOrg(t, db, "test-org", "Test Org")
		orgMemberID := testutil.CreateTestOrgMember(t, db, userID, orgID, organizations.RoleOwner)

		// Create session with the org member
		session, err := sessions.Create(ctx, db, userID, optional.Set(orgMemberID), nil)
		require.NoError(t, err)

		// Delete the org member to simulate it not existing
		query := `UPDATE org_members SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`
		_, err = db.Exec(ctx, query, orgMemberID)
		require.NoError(t, err)

		handlerCalled := false
		var capturedOrg *organizations.Organization
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedOrg = organizations.FromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Should still pass through, just without org context since org member is deleted
		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Nil(t, capturedOrg)
	})

	t.Run("session with org member but missing organization", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, nil)
		// Create valid org and org member
		orgID := testutil.CreateTestOrg(t, db, "test-org", "Test Org")
		orgMemberID := testutil.CreateTestOrgMember(t, db, userID, orgID, organizations.RoleOwner)

		// Create session with the org member
		session, err := sessions.Create(ctx, db, userID, optional.Set(orgMemberID), nil)
		require.NoError(t, err)

		// Delete the organization to simulate it not existing
		query := `UPDATE organizations SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1`
		_, err = db.Exec(ctx, query, orgID)
		require.NoError(t, err)

		handlerCalled := false
		var capturedOrg *organizations.Organization
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedOrg = organizations.FromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		// Should still pass through, just without org context since org is deleted
		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Nil(t, capturedOrg)
	})

	t.Run("session sets session ID in context", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		userID := testutil.CreateTestUser(t, db, nil)
		session, err := sessions.Create(ctx, db, userID, optional.Empty[int](), nil)
		require.NoError(t, err)

		// Get session ID from database
		retrieved, err := sessions.Get(ctx, db, session.Token)
		require.NoError(t, err)
		require.NotNil(t, retrieved)

		handlerCalled := false
		var capturedSessionID int
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			handlerCalled = true
			capturedSessionID = sessions.FromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})

		middleware := RequireSession(db)(handler)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(sessions.CreateCookie(session.Token))
		req = req.WithContext(log.WithContext(req.Context(), log.Default()))
		rec := httptest.NewRecorder()

		middleware.ServeHTTP(rec, req)

		require.True(t, handlerCalled)
		require.Equal(t, http.StatusOK, rec.Code)
		require.Equal(t, retrieved.ID, capturedSessionID)
	})
}
