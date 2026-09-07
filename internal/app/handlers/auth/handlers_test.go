package auth_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"nautilus/internal/app/handlers/auth"
	"nautilus/internal/crypto/totp"
	"nautilus/internal/database"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/recoverycodes"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/log"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

// mockCounter implements auth.Counter for testing
type mockCounter struct {
	count int
}

func (m *mockCounter) Count(_ context.Context, _ string) (int, time.Duration, error) {
	return m.count, 0, nil
}

func (m *mockCounter) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

// mfaTestUser holds credentials for a user with MFA enabled
type mfaTestUser struct {
	UserID        int
	Email         string
	Password      string
	TOTPSecret    string
	RecoveryCodes []string
}

// setupUserWithMFA creates a user with MFA enabled and returns their credentials
func setupUserWithMFA(t *testing.T, ctx context.Context, db database.Database, suffix string) *mfaTestUser {
	t.Helper()

	email := t.Name() + "_" + suffix + "@example.com"
	password := "password123"

	// Create user
	user, err := users.Register(ctx, db,
		optional.Set("testuser_"+t.Name()+"_"+suffix),
		optional.Set(email),
		password,
	)
	require.NoError(t, err)

	// Set up TOTP secret
	totpSecret := "JBSWY3DPEHPK3PXP"
	expiresAt := time.Now().Add(10 * time.Minute)
	err = users.SetPendingTOTP(ctx, db, user.ID, totpSecret, expiresAt)
	require.NoError(t, err)

	// Enable MFA
	err = users.EnableMFA(ctx, db, user.ID)
	require.NoError(t, err)

	// Generate recovery codes
	codes, err := recoverycodes.Generate(ctx, db, user.ID)
	require.NoError(t, err)

	return &mfaTestUser{
		UserID:        user.ID,
		Email:         email,
		Password:      password,
		TOTPSecret:    totpSecret,
		RecoveryCodes: codes,
	}
}

// generateTOTPCode generates a valid TOTP code for the given secret at the current time
func generateTOTPCode(t *testing.T, secret string) string {
	t.Helper()
	otp := totp.TOTP("Test", "test@example.com", secret)
	counter := uint64(time.Now().Unix() / 30)
	val, err := otp.Generate(counter)
	require.NoError(t, err)
	return totp.FormatValue(val, 6)
}

func TestAssume(t *testing.T) {
	t.Parallel()

	t.Run("admin can assume organization", func(t *testing.T) {
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

		// Create target organization
		orgID := testutil.CreateTestOrg(t, db, "target-org", "Target Org")
		org, err := organizations.Get(ctx, db, orgID)
		require.NoError(t, err)

		// Create admin session
		adminSession, err := sessions.Create(ctx, db, adminUser.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		// Get session from DB to get ID
		adminSessionFromDB, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)

		// Create the mux
		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		// Prepare request body
		body := map[string]string{"org_slug": org.Slug}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/assume", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		// Set context with admin user and session
		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		ctx = sessions.WithContext(ctx, adminSessionFromDB.ID)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Assume(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		// Check response
		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "organization assumed", response["message"])

		// Verify org in response
		orgResponse, ok := response["organization"].(map[string]interface{})
		require.True(t, ok)
		require.Equal(t, org.Slug, orgResponse["slug"])

		// Verify session was updated with assumed org
		updatedSession, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)
		require.True(t, updatedSession.AssumedOrgID.Set)
		require.Equal(t, orgID, updatedSession.AssumedOrgID.Data)
	})

	t.Run("assume with non-existent org returns error", func(t *testing.T) {
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

		// Create admin session
		adminSession, err := sessions.Create(ctx, db, adminUser.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		adminSessionFromDB, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)

		// Create the mux
		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		// Prepare request body with non-existent org
		body := map[string]string{"org_slug": "non-existent-org"}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/assume", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		ctx = sessions.WithContext(ctx, adminSessionFromDB.ID)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Assume(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to assume session", response["message"])
	})

	t.Run("assume with empty org slug returns error", func(t *testing.T) {
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

		// Create admin session
		adminSession, err := sessions.Create(ctx, db, adminUser.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		adminSessionFromDB, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"org_slug": ""}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/assume", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		ctx = sessions.WithContext(ctx, adminSessionFromDB.ID)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Assume(rec, req)

		require.Equal(t, http.StatusBadRequest, rec.Code)
	})
}

func TestUnassume(t *testing.T) {
	t.Parallel()

	t.Run("can unassume organization", func(t *testing.T) {
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

		// Create target organization
		orgID := testutil.CreateTestOrg(t, db, "unassume-org", "Unassume Org")

		// Create admin session
		adminSession, err := sessions.Create(ctx, db, adminUser.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		// Get session from DB to get ID
		adminSessionFromDB, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)

		// Assume the org
		err = sessions.AssumeOrg(ctx, db, adminSessionFromDB.ID, orgID)
		require.NoError(t, err)

		// Verify it's assumed
		assumedSession, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)
		require.True(t, assumedSession.AssumedOrgID.Set)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		req := httptest.NewRequest(http.MethodPost, "/unassume", nil)

		ctx = log.WithContext(ctx, log.Default())
		ctx = users.WithContext(ctx, adminUser)
		ctx = sessions.WithContext(ctx, adminSessionFromDB.ID)
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Unassume(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]interface{}
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "organization unassumed", response["message"])

		// Verify session no longer has assumed org
		unassumedSession, err := sessions.Get(ctx, db, adminSession.Token)
		require.NoError(t, err)
		require.False(t, unassumedSession.AssumedOrgID.Set)
	})
}

func TestLogin(t *testing.T) {
	t.Parallel()

	t.Run("successful login", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		email := t.Name() + "@example.com"
		password := "password123"

		// Create user
		_, err := users.Register(ctx, db,
			optional.Set("testuser_"+t.Name()),
			optional.Set(email),
			password,
		)
		require.NoError(t, err)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": email, "password": password}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Login successful", response["message"])
		require.NotNil(t, response["user"])

		// Check session cookie was set
		cookies := rec.Result().Cookies()
		var hasSessionCookie bool
		for _, c := range cookies {
			if c.Name == "nautilus-session" {
				hasSessionCookie = true
				require.NotEmpty(t, c.Value)
			}
		}
		require.True(t, hasSessionCookie, "Should have session cookie")
	})

	t.Run("already logged in", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		email := t.Name() + "@example.com"
		password := "password123"

		// Create user
		user, err := users.Register(ctx, db,
			optional.Set("testuser_"+t.Name()),
			optional.Set(email),
			password,
		)
		require.NoError(t, err)

		// Create existing session
		session, err := sessions.Create(ctx, db, user.ID, optional.Empty[int](), nil)
		require.NoError(t, err)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": email, "password": password}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(sessions.CreateCookie(session.Token))
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Already logged in", response["message"])
	})

	t.Run("invalid password", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		email := t.Name() + "@example.com"

		// Create user
		_, err := users.Register(ctx, db,
			optional.Set("testuser_"+t.Name()),
			optional.Set(email),
			"password123",
		)
		require.NoError(t, err)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": email, "password": "wrongpassword"}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})

	t.Run("non-existent user", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": "nonexistent@example.com", "password": "password123"}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})

	t.Run("empty email", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": "", "password": "password123"}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})

	t.Run("empty password", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{"email": "test@example.com", "password": ""}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})
}

func TestLoginWithMFA(t *testing.T) {
	t.Parallel()

	t.Run("MFA required when no code provided", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "mfa_required")

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		// Login without MFA code
		body := map[string]string{"email": mfaUser.Email, "password": mfaUser.Password}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])

		// Check for MFA required error code
		errors, ok := response["errors"].([]any)
		require.True(t, ok)
		require.Len(t, errors, 1)
		errDetail := errors[0].(map[string]any)
		require.Equal(t, "AUTH-27", errDetail["code"])
		require.Equal(t, "two-factor authentication required", errDetail["message"])
	})

	t.Run("successful login with valid TOTP code", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "totp_success")

		// Generate valid TOTP code
		totpCode := generateTOTPCode(t, mfaUser.TOTPSecret)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{
			"email":    mfaUser.Email,
			"password": mfaUser.Password,
			"code":     totpCode,
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Login successful", response["message"])
		require.NotNil(t, response["user"])

		// Check session cookie was set
		cookies := rec.Result().Cookies()
		var hasSessionCookie bool
		for _, c := range cookies {
			if c.Name == "nautilus-session" {
				hasSessionCookie = true
			}
		}
		require.True(t, hasSessionCookie, "Should have session cookie")
	})

	t.Run("invalid TOTP code", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "totp_invalid")

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{
			"email":    mfaUser.Email,
			"password": mfaUser.Password,
			"code":     "000000", // Invalid code
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])

		// Check for invalid TOTP code error
		errors, ok := response["errors"].([]any)
		require.True(t, ok)
		require.Len(t, errors, 1)
		errDetail := errors[0].(map[string]any)
		require.Equal(t, "AUTH-24", errDetail["code"])
	})

	t.Run("successful login with recovery code", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "recovery_success")

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		// Use first recovery code
		body := map[string]string{
			"email":    mfaUser.Email,
			"password": mfaUser.Password,
			"code":     mfaUser.RecoveryCodes[0],
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Login successful", response["message"])

		// Verify recovery code was consumed (count decreased)
		count, err := recoverycodes.CountRemaining(ctx, db, mfaUser.UserID)
		require.NoError(t, err)
		require.Equal(t, 9, count)
	})

	t.Run("already-used recovery code fails", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "recovery_used")

		// Use the recovery code once
		valid, err := recoverycodes.Verify(ctx, db, mfaUser.UserID, mfaUser.RecoveryCodes[0])
		require.NoError(t, err)
		require.True(t, valid)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		// Try to use the same recovery code for login
		body := map[string]string{
			"email":    mfaUser.Email,
			"password": mfaUser.Password,
			"code":     mfaUser.RecoveryCodes[0],
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})

	t.Run("invalid recovery code fails", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "recovery_invalid")

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{
			"email":    mfaUser.Email,
			"password": mfaUser.Password,
			"code":     "INVALIDCODE",
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})

	t.Run("wrong password with MFA user still fails", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := testutil.ContextWithEncrypter(t)

		// Set up user with MFA enabled
		mfaUser := setupUserWithMFA(t, ctx, db, "wrong_password")

		// Generate valid TOTP code
		totpCode := generateTOTPCode(t, mfaUser.TOTPSecret)

		mux := auth.NewMux(ctx, db, nil, &mockCounter{}, nil)

		body := map[string]string{
			"email":    mfaUser.Email,
			"password": "wrongpassword",
			"code":     totpCode,
		}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusUnauthorized, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])
	})
}

func TestLoginRateLimiting(t *testing.T) {
	t.Parallel()

	t.Run("too many failed attempts returns 429", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		email := t.Name() + "@example.com"
		password := "password123"

		// Create user
		_, err := users.Register(ctx, db,
			optional.Set("testuser_"+t.Name()),
			optional.Set(email),
			password,
		)
		require.NoError(t, err)

		// Mock counter that returns > maxLoginAttempts (5)
		counter := &mockCounter{count: 6}

		mux := auth.NewMux(ctx, db, nil, counter, nil)

		body := map[string]string{"email": email, "password": password}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		require.Equal(t, http.StatusTooManyRequests, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Unable to log in", response["message"])

		// Check for rate limit error
		errors, ok := response["errors"].([]any)
		require.True(t, ok)
		require.Len(t, errors, 1)
		errDetail := errors[0].(map[string]any)
		require.Equal(t, "AUTH-10", errDetail["code"])
		require.Contains(t, errDetail["message"], "too many failed login attempts")
	})

	t.Run("login allowed when under rate limit", func(t *testing.T) {
		t.Parallel()
		db := testutil.SetupTestDB(t)
		ctx := context.Background()

		email := t.Name() + "@example.com"
		password := "password123"

		// Create user
		_, err := users.Register(ctx, db,
			optional.Set("testuser_"+t.Name()),
			optional.Set(email),
			password,
		)
		require.NoError(t, err)

		// Mock counter that returns <= maxLoginAttempts (5)
		counter := &mockCounter{count: 4}

		mux := auth.NewMux(ctx, db, nil, counter, nil)

		body := map[string]string{"email": email, "password": password}
		bodyBytes, err := json.Marshal(body)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(bodyBytes))
		req.Header.Set("Content-Type", "application/json")
		ctx = log.WithContext(ctx, log.Default())
		req = req.WithContext(ctx)

		rec := httptest.NewRecorder()
		mux.Login(rec, req)

		// Should succeed (not rate limited)
		require.Equal(t, http.StatusOK, rec.Code)

		var response map[string]any
		err = json.Unmarshal(rec.Body.Bytes(), &response)
		require.NoError(t, err)
		require.Equal(t, "Login successful", response["message"])
	})
}
