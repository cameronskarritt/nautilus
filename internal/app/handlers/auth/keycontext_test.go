package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"nautilus/internal/app/handlers/auth"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestLoginResolvesSharedKeyAfterPasswordVerification(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing code", "wrong password", "TOTP", "recovery", "provider unavailable"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			keys := &authKeys{key: bytes.Repeat([]byte{5}, 32)}
			user := setupUserWithMFA(t, encrypt.WithContext(t.Context(), encrypt.ForUser(keys)), db, "kms")
			keys.userCalls = 0
			router := mux.New()
			auth.NewMux(t.Context(), db, nil, &mockCounter{}, keys).Mount(router, "/auth")
			body := map[string]string{
				"email": user.Email, "password": user.Password, "code": generateTOTPCode(t, user.TOTPSecret),
			}
			wantStatus, wantCalls := http.StatusOK, 1
			switch name {
			case "missing code":
				delete(body, "code")
				wantStatus, wantCalls = http.StatusUnauthorized, 0
			case "wrong password":
				body["password"] = "wrong-password"
				wantStatus, wantCalls = http.StatusUnauthorized, 0
			case "recovery":
				body["code"] = user.RecoveryCodes[0]
			case "provider unavailable":
				keys.err = errors.New("key provider unavailable")
				wantStatus = http.StatusInternalServerError
			}
			rec := serveAuthJSON(t, router, "/auth/sessions", body, nil)
			require.Equal(t, wantStatus, rec.Code)
			require.Equal(t, wantCalls, keys.userCalls)
			require.Zero(t, keys.orgCalls)
			if wantStatus == http.StatusOK {
				require.NotEmpty(t, rec.Result().Cookies())
			} else {
				require.Empty(t, rec.Result().Cookies())
				require.NotContains(t, rec.Body.String(), "key provider unavailable")
			}
		})
	}
}

func TestTOTPSetupSurvivesOrganizationSwitch(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	userID := testutil.CreateTestUser(t, db, nil)
	first := testutil.CreateTestOrg(t, db, "auth-keys-first", "First")
	second := testutil.CreateTestOrg(t, db, "auth-keys-second", "Second")
	firstMember := testutil.CreateTestOrgMember(t, db, userID, first, organizations.RoleOwner)
	secondMember := testutil.CreateTestOrgMember(t, db, userID, second, organizations.RoleMember)
	session, err := sessions.Create(ctx, db, userID, optional.Set(firstMember), nil)
	require.NoError(t, err)
	storedSession, err := sessions.Get(ctx, db, session.Token)
	require.NoError(t, err)
	keys := &authKeys{key: bytes.Repeat([]byte{5}, 32)}
	router := mux.New()
	auth.NewMux(ctx, db, nil, &mockCounter{}, keys).Mount(router, "/auth")
	cookie := sessions.CreateCookie(session.Token)
	rec := serveAuthJSON(t, router, "/auth/totp/request", map[string]string{"password": "password123"}, cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	var response struct {
		URI string `json:"uri"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	uri, err := url.Parse(response.URI)
	require.NoError(t, err)
	secret := uri.Query().Get("secret")
	require.NotEmpty(t, secret)
	require.NoError(t, sessions.SwitchOrg(ctx, db, storedSession.ID, secondMember))
	rec = serveAuthJSON(t, router, "/auth/totp/complete", map[string]string{"code": generateTOTPCode(t, secret)}, cookie)
	require.Equal(t, http.StatusOK, rec.Code)
	user, err := users.Get(ctx, db, userID)
	require.NoError(t, err)
	require.True(t, user.MFAEnabled)
	require.Equal(t, 2, keys.userCalls)
	require.Zero(t, keys.orgCalls)
	got, err := users.GetTOTPSecret(encrypt.WithContext(ctx, encrypt.ForUser(keys)), db, userID)
	require.NoError(t, err)
	require.Equal(t, secret, got)
}

func serveAuthJSON(t *testing.T, handler http.Handler, path string, body map[string]string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(body)
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(data)).WithContext(t.Context())
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Organization-Slug", "untrusted-org")
	if cookie != nil {
		req.AddCookie(cookie)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

type authKeys struct {
	key       []byte
	err       error
	userCalls int
	orgCalls  int
}

func (k *authKeys) UserKey(context.Context) ([]byte, error) {
	k.userCalls++
	return bytes.Clone(k.key), k.err
}

func (k *authKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	k.orgCalls++
	return nil, errors.New("authentication must not request organization keys")
}
