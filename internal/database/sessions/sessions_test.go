package sessions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/sessions"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func createStoredSession(t *testing.T, ctx context.Context, db database.Database, userID int) (string, *sessions.Session) {
	t.Helper()

	created, err := sessions.Create(ctx, db, userID, optional.Empty[int](), nil)
	require.NoError(t, err)

	session, err := sessions.Get(ctx, db, created.Token)
	require.NoError(t, err)
	require.NotNil(t, session)

	return created.Token, session
}

func TestCreate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		metadata          *sessions.SessionMetadata
		expectedAddr      optional.Optional[string]
		expectedUserAgent optional.Optional[string]
	}{
		{
			name: "with metadata",
			metadata: &sessions.SessionMetadata{
				Addr:      optional.Set("192.168.1.1:12345"),
				UserAgent: optional.Set("TestBrowser/1.0"),
			},
			expectedAddr:      optional.Set("192.168.1.1:12345"),
			expectedUserAgent: optional.Set("TestBrowser/1.0"),
		},
		{
			name:              "without metadata",
			metadata:          nil,
			expectedAddr:      optional.Empty[string](),
			expectedUserAgent: optional.Empty[string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := context.Background()

			userID := testutil.CreateTestUser(t, db, nil)
			startedAt := time.Now()

			session, err := sessions.Create(ctx, db, userID, optional.Empty[int](), tt.metadata)

			require.NoError(t, err)
			require.NotNil(t, session)
			require.NotEmpty(t, session.Token)
			require.False(t, session.OrgMemberID.Set)
			require.False(t, session.AssumedBy.Set)
			require.True(t, session.ExpiresAt.After(startedAt))
			require.True(t, session.ExpiresAt.Before(startedAt.Add(31*24*time.Hour)))

			var addr, userAgent optional.Optional[string]
			err = db.QueryRow(ctx, `SELECT ip_addr, user_agent FROM sessions WHERE user_id = $1`, userID).Scan(&addr, &userAgent)
			require.NoError(t, err)
			require.Equal(t, tt.expectedAddr, addr)
			require.Equal(t, tt.expectedUserAgent, userAgent)
		})
	}
}

func TestAssumeOrg(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "test-org", "Test Org")
	token, session := createStoredSession(t, ctx, db, userID)

	require.False(t, session.AssumedOrgID.Set)

	err := sessions.AssumeOrg(ctx, db, session.ID, orgID)
	require.NoError(t, err)

	updated, err := sessions.Get(ctx, db, token)
	require.NoError(t, err)
	require.True(t, updated.AssumedOrgID.Set)
	require.Equal(t, orgID, updated.AssumedOrgID.Data)
}

func TestUnassumeOrg(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "test-unassume-org", "Test Unassume Org")
	token, session := createStoredSession(t, ctx, db, userID)

	err := sessions.AssumeOrg(ctx, db, session.ID, orgID)
	require.NoError(t, err)

	assumed, err := sessions.Get(ctx, db, token)
	require.NoError(t, err)
	require.True(t, assumed.AssumedOrgID.Set)

	err = sessions.UnassumeOrg(ctx, db, session.ID)
	require.NoError(t, err)

	unassumed, err := sessions.Get(ctx, db, token)
	require.NoError(t, err)
	require.False(t, unassumed.AssumedOrgID.Set)
}

func TestGet_NotFound(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	session, err := sessions.Get(ctx, db, strings.Repeat("A", 86))
	require.NoError(t, err)
	require.Nil(t, session)
}

func TestTouch(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)

	created, err := sessions.Create(ctx, db, userID, optional.Empty[int](), nil)
	require.NoError(t, err)
	originalExpiry := created.ExpiresAt

	time.Sleep(10 * time.Millisecond)

	touched, err := sessions.Touch(ctx, db, created.Token)
	require.NoError(t, err)
	require.NotNil(t, touched)
	require.Equal(t, created.Token, touched.Token)
	require.True(t, touched.ExpiresAt.After(originalExpiry) || touched.ExpiresAt.Equal(originalExpiry))
}

func TestEnd(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	token, session := createStoredSession(t, ctx, db, userID)

	err := sessions.End(ctx, db, session.ID)
	require.NoError(t, err)

	ended, err := sessions.Get(ctx, db, token)
	require.NoError(t, err)
	require.Nil(t, ended)
}

func TestEndAllExcept(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	session1Token, _ := createStoredSession(t, ctx, db, userID)
	session2Token, session2 := createStoredSession(t, ctx, db, userID)
	session3Token, _ := createStoredSession(t, ctx, db, userID)

	err := sessions.EndAllExcept(ctx, db, userID, session2.ID)
	require.NoError(t, err)

	got1, err := sessions.Get(ctx, db, session1Token)
	require.NoError(t, err)
	require.Nil(t, got1)

	got2, err := sessions.Get(ctx, db, session2Token)
	require.NoError(t, err)
	require.NotNil(t, got2)

	got3, err := sessions.Get(ctx, db, session3Token)
	require.NoError(t, err)
	require.Nil(t, got3)
}

func TestEndAll(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	userID := testutil.CreateTestUser(t, db, nil)
	session1Token, _ := createStoredSession(t, ctx, db, userID)
	session2Token, _ := createStoredSession(t, ctx, db, userID)

	err := sessions.EndAll(ctx, db, userID)
	require.NoError(t, err)

	got1, err := sessions.Get(ctx, db, session1Token)
	require.NoError(t, err)
	require.Nil(t, got1)

	got2, err := sessions.Get(ctx, db, session2Token)
	require.NoError(t, err)
	require.Nil(t, got2)
}

func TestRequestMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		remoteAddr        string
		userAgent         string
		expectedAddr      optional.Optional[string]
		expectedUserAgent optional.Optional[string]
	}{
		{
			name:              "with both addr and user agent",
			remoteAddr:        "192.168.1.100:54321",
			userAgent:         "Mozilla/5.0 TestBrowser",
			expectedAddr:      optional.Set("192.168.1.100:54321"),
			expectedUserAgent: optional.Set("Mozilla/5.0 TestBrowser"),
		},
		{
			name:              "with only addr",
			remoteAddr:        "10.0.0.1:8080",
			expectedAddr:      optional.Set("10.0.0.1:8080"),
			expectedUserAgent: optional.Empty[string](),
		},
		{
			name:              "with only user agent",
			userAgent:         "curl/7.64.1",
			expectedAddr:      optional.Empty[string](),
			expectedUserAgent: optional.Set("curl/7.64.1"),
		},
		{
			name:              "with neither",
			expectedAddr:      optional.Empty[string](),
			expectedUserAgent: optional.Empty[string](),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			req.RemoteAddr = tt.remoteAddr
			if tt.userAgent != "" {
				req.Header.Set("User-Agent", tt.userAgent)
			}

			meta := sessions.RequestMetadata(req)

			require.NotNil(t, meta)
			require.Equal(t, tt.expectedAddr, meta.Addr)
			require.Equal(t, tt.expectedUserAgent, meta.UserAgent)
		})
	}
}
