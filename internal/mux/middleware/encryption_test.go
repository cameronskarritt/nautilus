package middleware_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/api/authentication"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

type keyManager struct {
	organizations []string
	users         int
	err           error
}

func (m *keyManager) OrganizationKey(_ context.Context, orgID string) ([]byte, error) {
	m.organizations = append(m.organizations, orgID)
	key := sha256.Sum256([]byte(orgID))
	return key[:], m.err
}

func (m *keyManager) UserKey(context.Context) ([]byte, error) {
	m.users++
	return bytes.Repeat([]byte{1}, 32), m.err
}

func TestUserEncryption(t *testing.T) {
	t.Parallel()
	manager := new(keyManager)
	router := mux.New()
	router.Use(middleware.UserEncryption(manager))
	router.Get("/unused", func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, encrypt.FromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	})
	router.Get("/encrypt", func(w http.ResponseWriter, r *http.Request) {
		enc := encrypt.FromContext(r.Context())
		ciphertext, err := enc.Seal(r.Context(), []byte("secret"), encrypt.Binding{Purpose: "test", RecordID: "record"})
		if manager.err != nil {
			require.ErrorIs(t, err, manager.err)
			require.Empty(t, ciphertext)
		} else {
			require.NoError(t, err)
			plaintext, err := enc.Open(r.Context(), ciphertext, encrypt.Binding{Purpose: "test", RecordID: "record"})
			require.NoError(t, err)
			require.Equal(t, []byte("secret"), plaintext)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/unused", nil))
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Zero(t, manager.users)
	require.Empty(t, manager.organizations)

	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/encrypt", nil))
	require.Positive(t, manager.users)
	require.Empty(t, manager.organizations)
	manager.err = errors.New("key provider unavailable")
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/encrypt", nil))
}

func TestOrganizationEncryptionMasksUnauthorizedContext(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"missing organization", "missing external ID", "missing authentication", "missing member",
		"synthetic member", "wrong user", "wrong organization", "missing session",
		"wrong API organization", "unpersisted API key",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manager := new(keyManager)
			org := &organizations.Organization{ID: 1, ExternalID: "organization"}
			member := &organizations.Member{ID: 1, OrganizationID: 1, UserID: 1}
			user := &users.User{ID: 1}
			sessionID := 1
			var key *apikeys.Key
			switch name {
			case "missing organization":
				org = nil
			case "missing external ID":
				org.ExternalID = ""
			case "missing authentication":
				user = nil
			case "missing member":
				member = nil
			case "synthetic member":
				member.ID = 0
			case "wrong user":
				member.UserID = 2
			case "wrong organization":
				member.OrganizationID = 2
			case "missing session":
				sessionID = 0
			case "wrong API organization":
				key = &apikeys.Key{ID: 1, OrganizationID: 2}
			case "unpersisted API key":
				key = &apikeys.Key{OrganizationID: 1}
			}
			ctx := organizations.WithContext(t.Context(), org)
			ctx = organizations.WithMemberContext(ctx, member)
			ctx = users.WithContext(ctx, user)
			ctx = sessions.WithContext(ctx, sessionID)
			ctx = apikeys.WithContext(ctx, key)
			router := mux.New()
			router.Use(middleware.UserEncryption(manager), middleware.OrganizationEncryption(manager))
			router.Get("/account", func(w http.ResponseWriter, r *http.Request) {
				require.Nil(t, encrypt.FromContext(r.Context()))
				w.WriteHeader(http.StatusNoContent)
			})
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/account", nil).WithContext(ctx))
			require.Equal(t, http.StatusNoContent, rec.Code)
			require.Empty(t, manager.organizations)
			require.Zero(t, manager.users)
		})
	}
}

func TestOrganizationEncryptionFollowsSessionSwitch(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	firstID := testutil.CreateTestOrg(t, db, t.Name()+"first", "First")
	secondID := testutil.CreateTestOrg(t, db, t.Name()+"second", "Second")
	firstMember := testutil.CreateTestOrgMember(t, db, userID, firstID, organizations.RoleOwner)
	secondMember := testutil.CreateTestOrgMember(t, db, userID, secondID, organizations.RoleOwner)
	first, err := organizations.Get(t.Context(), db, firstID)
	require.NoError(t, err)
	second, err := organizations.Get(t.Context(), db, secondID)
	require.NoError(t, err)
	session, err := sessions.Create(t.Context(), db, userID, optional.Set(firstMember), nil)
	require.NoError(t, err)
	stored, err := sessions.Get(t.Context(), db, session.Token)
	require.NoError(t, err)
	manager := new(keyManager)
	var ciphertext []byte
	var captured *encrypt.Encrypter
	router := mux.New()
	router.Use(middleware.UserEncryption(manager), middleware.RequireSession(db), middleware.AdminOrgOverride(db), middleware.OrganizationEncryption(manager))
	router.Get("/document", func(w http.ResponseWriter, r *http.Request) {
		captured = encrypt.FromContext(r.Context())
		require.NotNil(t, captured)
		ciphertext, err = captured.Seal(r.Context(), []byte("document"), encrypt.Binding{Purpose: "test", RecordID: "record"})
		require.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	})
	req := httptest.NewRequest(http.MethodGet, "/document", nil)
	req.AddCookie(sessions.CreateCookie(session.Token))
	req.Header.Set("X-Organization-Slug", second.Slug)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{first.ExternalID}, manager.organizations)
	firstCiphertext := bytes.Clone(ciphertext)
	firstEncrypter := captured

	require.NoError(t, sessions.SwitchOrg(t.Context(), db, stored.ID, secondMember))
	router.ServeHTTP(httptest.NewRecorder(), req)
	require.Equal(t, []string{first.ExternalID, second.ExternalID}, manager.organizations)
	_, err = captured.Open(t.Context(), firstCiphertext, encrypt.Binding{Purpose: "test", RecordID: "record"})
	require.Error(t, err)
	plaintext, err := firstEncrypter.Open(t.Context(), firstCiphertext, encrypt.Binding{Purpose: "test", RecordID: "record"})
	require.NoError(t, err)
	require.Equal(t, []byte("document"), plaintext)
	require.Zero(t, manager.users)
}

func TestOrganizationEncryptionUnavailableSessionScopes(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"no organization", "deleted member", "deleted organization", "admin header", "admin assumption"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			userID := testutil.CreateTestUser(t, db, &testutil.TestUserOptions{Admin: true})
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Organization")
			memberID := testutil.CreateTestOrgMember(t, db, userID, orgID, organizations.RoleOwner)
			member := optional.Set(memberID)
			if name == "no organization" {
				member = optional.Empty[int]()
			}
			session, err := sessions.Create(t.Context(), db, userID, member, nil)
			require.NoError(t, err)
			switch name {
			case "deleted member":
				_, err = db.Exec(t.Context(), "UPDATE org_members SET deleted_at = CURRENT_TIMESTAMP WHERE id = $1", memberID)
				require.NoError(t, err)
			case "deleted organization":
				require.NoError(t, organizations.Delete(t.Context(), db, orgID))
			case "admin assumption":
				stored, err := sessions.Get(t.Context(), db, session.Token)
				require.NoError(t, err)
				require.NoError(t, sessions.AssumeOrg(t.Context(), db, stored.ID, orgID))
			}
			manager := new(keyManager)
			router := mux.New()
			router.Use(middleware.UserEncryption(manager), middleware.RequireSession(db), middleware.AdminOrgOverride(db), middleware.OrganizationEncryption(manager))
			router.Get("/account", func(w http.ResponseWriter, r *http.Request) {
				require.Nil(t, encrypt.FromContext(r.Context()))
				w.WriteHeader(http.StatusNoContent)
			})
			req := httptest.NewRequest(http.MethodGet, "/account", nil)
			req.AddCookie(sessions.CreateCookie(session.Token))
			if name == "admin header" {
				req.Header.Set("X-Organization-Slug", t.Name())
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusNoContent, rec.Code)
			require.Empty(t, manager.organizations)
			require.Zero(t, manager.users)
		})
	}
}

func TestOrganizationEncryptionUsesAuthenticatedAPIKey(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, t.Name(), "Organization")
	otherID := testutil.CreateTestOrg(t, db, t.Name()+"other", "Other")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	other, err := organizations.Get(t.Context(), db, otherID)
	require.NoError(t, err)
	_, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{Name: "Key", Scopes: []apikeys.Scope{apikeys.ScopeRead}})
	require.NoError(t, err)
	manager := new(keyManager)
	router := mux.New()
	router.Use(middleware.UserEncryption(manager), authentication.RequireAPIKey(db), middleware.OrganizationEncryption(manager))
	router.Get("/unused", func(w http.ResponseWriter, r *http.Request) {
		require.NotNil(t, encrypt.FromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	})
	router.Get("/document", func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, orgID, organizations.FromContext(r.Context()).ID)
		enc := encrypt.FromContext(r.Context())
		require.NotNil(t, enc)
		_, err := enc.Seal(r.Context(), []byte("document"), encrypt.Binding{Purpose: "test", RecordID: "record"})
		require.NoError(t, err)
		w.WriteHeader(http.StatusNoContent)
	})
	for _, path := range []string{"/unused", "/document"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("X-Organization-Slug", other.Slug)
		req = req.WithContext(organizations.WithContext(req.Context(), other))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, http.StatusNoContent, rec.Code)
		if path == "/unused" {
			require.Empty(t, manager.organizations)
		}
	}
	require.Equal(t, []string{org.ExternalID}, manager.organizations)
	require.Zero(t, manager.users)
}
