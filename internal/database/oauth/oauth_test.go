package oauth_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"nautilus/internal/database"
	"nautilus/internal/database/oauth"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

const resource = "https://mcp.example.com/mcp"
const redirect = "http://127.0.0.1:7777/callback"

var verifier = strings.Repeat("a", 43)

type fixture struct {
	db                      database.Database
	client                  *oauth.Client
	code                    string
	userID, memberID, orgID int
}

func setup(t *testing.T, db database.Database) fixture {
	t.Helper()
	f := fixture{db: db}
	f.userID = testutil.CreateTestUser(t, db, nil)
	f.orgID = testutil.CreateTestOrg(t, db, "oauth", "OAuth")
	f.memberID = testutil.CreateTestOrgMember(t, db, f.userID, f.orgID, enums.RoleMember)
	var err error
	f.client, err = oauth.RegisterClient(t.Context(), db, "Test", []string{redirect})
	require.NoError(t, err)
	challenge := sha256.Sum256([]byte(verifier))
	f.code, err = oauth.CreateGrant(t.Context(), db, f.userID, f.memberID, &oauth.GrantOptions{ClientID: f.client.ID, RedirectURI: redirect, Resource: resource, Scope: "read write", Challenge: base64.RawURLEncoding.EncodeToString(challenge[:])})
	require.NoError(t, err)
	return f
}
func (f fixture) exchange(t *testing.T) *oauth.Tokens {
	t.Helper()
	tokens, err := oauth.ExchangeCode(t.Context(), f.db, f.client.ID, f.code, redirect, resource, verifier)
	require.NoError(t, err)
	return tokens
}
func TestExchangeBindings(t *testing.T) {
	t.Parallel()
	for _, field := range []string{"client", "redirect", "resource", "verifier", "expired"} {
		t.Run(field, func(t *testing.T) {
			t.Parallel()
			f := setup(t, testutil.SetupTestDB(t))
			clientID, uri, res, v := f.client.ID, redirect, resource, verifier
			switch field {
			case "client":
				clientID = "different"
			case "redirect":
				uri += "/different"
			case "resource":
				res += "/different"
			case "verifier":
				v = strings.Repeat("b", 43)
			case "expired":
				_, err := f.db.Exec(t.Context(), `UPDATE mcp_oauth_codes SET expires_at = CURRENT_TIMESTAMP - INTERVAL '1 minute'`)
				require.NoError(t, err)
			}
			tokens, err := oauth.ExchangeCode(t.Context(), f.db, clientID, f.code, uri, res, v)
			require.ErrorIs(t, err, oauth.ErrInvalidGrant)
			require.Nil(t, tokens)
			if field != "expired" {
				f.exchange(t)
			}
		})
	}
}
func TestTokenLifecycle(t *testing.T) {
	t.Parallel()
	f := setup(t, testutil.SetupTestDB(t))
	ctx := t.Context()
	client, err := oauth.GetClient(ctx, f.db, f.client.ID)
	require.NoError(t, err)
	require.Equal(t, f.client, client)
	tokens := f.exchange(t)
	grant, err := oauth.Authenticate(ctx, f.db, tokens.AccessToken, resource)
	require.NoError(t, err)
	require.NotNil(t, grant)
	require.Equal(t, f.userID, grant.UserID)
	require.Equal(t, f.memberID, grant.MemberID)
	require.Equal(t, f.orgID, grant.OrganizationID)
	data, err := json.Marshal(grant)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(data))
	require.Equal(t, grant, oauth.FromContext(oauth.WithContext(ctx, grant)))
	var accessHash, refreshHash, codeHash []byte
	require.NoError(t, f.db.QueryRow(ctx, `SELECT t.access_hash,t.refresh_hash,c.hash FROM mcp_oauth_tokens t JOIN mcp_oauth_codes c ON c.grant_id=t.grant_id WHERE t.grant_id=$1`, grant.ID).Scan(&accessHash, &refreshHash, &codeHash))
	for _, pair := range []struct {
		raw  string
		hash []byte
	}{{tokens.AccessToken, accessHash}, {tokens.RefreshToken, refreshHash}, {f.code, codeHash}} {
		sum := sha256.Sum256([]byte(pair.raw))
		require.Equal(t, sum[:], pair.hash)
	}
	bad, err := oauth.Authenticate(ctx, f.db, tokens.AccessToken, resource+"/other")
	require.NoError(t, err)
	require.Nil(t, bad)
	_, err = oauth.Refresh(ctx, f.db, "other", tokens.RefreshToken, resource, "")
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
	_, err = oauth.Refresh(ctx, f.db, f.client.ID, tokens.RefreshToken, resource+"/other", "")
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
	narrowed, err := oauth.Refresh(ctx, f.db, f.client.ID, tokens.RefreshToken, resource, "read")
	require.NoError(t, err)
	require.Equal(t, "read", narrowed.Scope)
	_, err = oauth.Refresh(ctx, f.db, f.client.ID, narrowed.RefreshToken, resource, "read write")
	require.ErrorIs(t, err, oauth.ErrInvalidScope)
	newest, err := oauth.Refresh(ctx, f.db, f.client.ID, narrowed.RefreshToken, resource, "")
	require.NoError(t, err)
	require.Equal(t, "read", newest.Scope)
	_, err = oauth.Refresh(ctx, f.db, f.client.ID, tokens.RefreshToken, resource, "")
	require.ErrorIs(t, err, oauth.ErrInvalidGrant)
	for _, token := range []string{tokens.AccessToken, narrowed.AccessToken, newest.AccessToken} {
		got, err := oauth.Authenticate(ctx, f.db, token, resource)
		require.NoError(t, err)
		require.Nil(t, got)
	}
}
func TestInvalidation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"code replay", "revoke access", "revoke refresh", "access expired", "family expired", "member deleted", "user deleted", "org deleted", "role downgraded"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t, testutil.SetupTestDB(t))
			ctx := t.Context()
			tokens := f.exchange(t)
			switch mode {
			case "code replay":
				_, err := oauth.ExchangeCode(ctx, f.db, f.client.ID, f.code, redirect, resource, verifier)
				require.ErrorIs(t, err, oauth.ErrInvalidGrant)
			case "revoke access":
				require.NoError(t, oauth.Revoke(ctx, f.db, f.client.ID, tokens.AccessToken))
			case "revoke refresh":
				require.NoError(t, oauth.Revoke(ctx, f.db, f.client.ID, tokens.RefreshToken))
			default:
				queries := map[string]string{
					"access expired":  `UPDATE mcp_oauth_tokens SET access_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 minute'`,
					"family expired":  `UPDATE mcp_oauth_grants SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 minute'`,
					"member deleted":  `UPDATE org_members SET deleted_at=CURRENT_TIMESTAMP`,
					"user deleted":    `UPDATE users SET deleted_at=CURRENT_TIMESTAMP`,
					"org deleted":     `UPDATE organizations SET deleted_at=CURRENT_TIMESTAMP`,
					"role downgraded": `UPDATE org_members SET role='viewer'`,
				}
				_, err := f.db.Exec(ctx, queries[mode])
				require.NoError(t, err)
			}
			got, err := oauth.Authenticate(ctx, f.db, tokens.AccessToken, resource)
			require.NoError(t, err)
			require.Nil(t, got)
			if mode != "access expired" {
				_, err = oauth.Refresh(ctx, f.db, f.client.ID, tokens.RefreshToken, resource, "")
				require.ErrorIs(t, err, oauth.ErrInvalidGrant)
			}
		})
	}
}
func TestRevocationClientBoundary(t *testing.T) {
	t.Parallel()
	f := setup(t, testutil.SetupTestDB(t))
	ctx := t.Context()
	tokens := f.exchange(t)
	require.NoError(t, oauth.Revoke(ctx, f.db, "another", tokens.AccessToken))
	require.NoError(t, oauth.Revoke(ctx, f.db, f.client.ID, "unknown"))
	got, err := oauth.Authenticate(ctx, f.db, tokens.AccessToken, resource)
	require.NoError(t, err)
	require.NotNil(t, got)
}
func TestCreateGrantMembershipAndScope(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"wrong user", "viewer write", "invalid scope", "invalid challenge", "unknown client"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t, testutil.SetupTestDB(t))
			ctx := t.Context()
			sum := sha256.Sum256([]byte(verifier))
			opts := &oauth.GrantOptions{ClientID: f.client.ID, RedirectURI: redirect, Resource: resource, Scope: "read write", Challenge: base64.RawURLEncoding.EncodeToString(sum[:])}
			want := oauth.ErrInvalidGrant
			switch mode {
			case "wrong user":
				f.userID++
			case "viewer write":
				_, err := f.db.Exec(ctx, `UPDATE org_members SET role='viewer' WHERE id=$1`, f.memberID)
				require.NoError(t, err)
				want = oauth.ErrInvalidScope
			case "invalid scope":
				opts.Scope = "read admin"
				want = oauth.ErrInvalidScope
			case "invalid challenge":
				opts.Challenge = "abc"
			case "unknown client":
				opts.ClientID = "unknown"
				want = oauth.ErrInvalidClient
			}
			code, err := oauth.CreateGrant(ctx, f.db, f.userID, f.memberID, opts)
			require.ErrorIs(t, err, want)
			require.Empty(t, code)
		})
	}
}
func TestConcurrentRefreshRevokesFamily(t *testing.T) {
	t.Parallel()
	f := setup(t, testutil.SetupTestDBWithCommit(t))
	tokens := f.exchange(t)
	var wg sync.WaitGroup
	results := make([]*oauth.Tokens, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Go(func() {
			results[i], errs[i] = oauth.Refresh(t.Context(), f.db, f.client.ID, tokens.RefreshToken, resource, "")
		})
	}
	wg.Wait()
	successes := 0
	for i, err := range errs {
		if err == nil {
			successes++
			got, e := oauth.Authenticate(t.Context(), f.db, results[i].AccessToken, resource)
			require.NoError(t, e)
			require.Nil(t, got)
		} else {
			require.ErrorIs(t, err, oauth.ErrInvalidGrant)
		}
	}
	require.Equal(t, 1, successes)
}

func TestAuthenticateRejectsMalformedTokensWithoutDatabase(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct{ name, token string }{
		{"empty", ""},
		{"session", "session-token"},
		{"API key", "sk_" + strings.Repeat("A", 43)},
		{"refresh token", "mcp_rt_" + strings.Repeat("A", 43)},
		{"short", "mcp_at_" + strings.Repeat("A", 42)},
		{"long", "mcp_at_" + strings.Repeat("A", 44)},
		{"invalid base64", "mcp_at_" + strings.Repeat("!", 43)},
		{"noncanonical base64", "mcp_at_" + strings.Repeat("A", 42) + "B"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			grant, err := oauth.Authenticate(t.Context(), nil, tt.token, resource)
			require.NoError(t, err)
			require.Nil(t, grant)
		})
	}
}

func TestAuthenticateHashSharesTokenValidation(t *testing.T) {
	t.Parallel()
	f := setup(t, testutil.SetupTestDB(t))
	ctx := t.Context()
	tokens := f.exchange(t)
	sum := sha256.Sum256([]byte(tokens.AccessToken))
	grant, err := oauth.Authenticate(ctx, f.db, tokens.AccessToken, resource)
	require.NoError(t, err)
	require.Equal(t, sum[:], grant.AccessHash)
	byHash, err := oauth.AuthenticateHash(ctx, f.db, sum[:], resource)
	require.NoError(t, err)
	require.Equal(t, grant, byHash)
	sum[0] ^= 1
	require.Equal(t, grant.AccessHash, byHash.AccessHash)
	require.NoError(t, oauth.Revoke(ctx, f.db, f.client.ID, tokens.AccessToken))
	got, err := oauth.AuthenticateHash(ctx, f.db, grant.AccessHash, resource)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = oauth.AuthenticateHash(ctx, nil, []byte("short"), resource)
	require.NoError(t, err)
	require.Nil(t, got)
}
