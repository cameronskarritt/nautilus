package documentdownloads_test

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documentdownloads"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/oauth"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

const resource = "https://mcp.example.com/mcp"

type fixture struct {
	db               database.Database
	doc              *documents.Document
	key              *apikeys.Key
	client           *oauth.Client
	tokens           *oauth.Tokens
	userID, memberID int
}

func setup(t *testing.T) fixture {
	t.Helper()
	f := fixture{db: testutil.SetupTestDB(t)}
	ctx := t.Context()
	f.userID = testutil.CreateTestUser(t, f.db, nil)
	orgID := testutil.CreateTestOrg(t, f.db, "downloads", "Downloads")
	f.memberID = testutil.CreateTestOrgMember(t, f.db, f.userID, orgID, enums.RoleMember)
	var err error
	f.doc, err = documents.Create(ctx, f.db, orgID, &documents.CreateOptions{Filename: "report.pdf", ContentType: "application/pdf", Size: 42})
	require.NoError(t, err)
	f.doc, err = documents.MarkUploaded(ctx, f.db, orgID, f.doc.ExternalID)
	require.NoError(t, err)
	f.key, _, err = apikeys.Create(ctx, f.db, orgID, f.userID, &apikeys.CreateOptions{Name: "Download", Scopes: []enums.Scope{enums.ScopeRead, enums.ScopeWrite}})
	require.NoError(t, err)
	f.client, err = oauth.RegisterClient(ctx, f.db, "Reader", []string{"https://client.example.com/callback"})
	require.NoError(t, err)
	verifier := strings.Repeat("a", 43)
	challenge := sha256.Sum256([]byte(verifier))
	code, err := oauth.CreateGrant(ctx, f.db, f.userID, f.memberID, &oauth.GrantOptions{
		ClientID: f.client.ID, RedirectURI: f.client.RedirectURIs[0], Resource: resource, Scope: "read write",
		Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
	})
	require.NoError(t, err)
	f.tokens, err = oauth.ExchangeCode(ctx, f.db, f.client.ID, code, f.client.RedirectURIs[0], resource, verifier)
	require.NoError(t, err)
	return f
}

func (f fixture) options(kind string) *documentdownloads.CreateOptions {
	opts := &documentdownloads.CreateOptions{Resource: resource}
	if kind == "api" {
		opts.APIKeyID = f.key.ID
	} else {
		hash := sha256.Sum256([]byte(f.tokens.AccessToken))
		opts.OAuthAccessHash = hash[:]
	}
	return opts
}

func TestCreateStoresOnlyHashAndResolvesUploadedDocument(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"api", "oauth"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			before := time.Now()
			token, expires, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, f.options(kind))
			require.NoError(t, err)
			require.True(t, strings.HasPrefix(token, "mcp_dl_"))
			require.Len(t, token, len("mcp_dl_")+43)
			require.WithinDuration(t, before.Add(5*time.Minute), expires, 5*time.Second)
			var stored []byte
			var storedExpires time.Time
			require.NoError(t, f.db.QueryRow(ctx, `SELECT token_hash, expires_at FROM document_downloads`).Scan(&stored, &storedExpires))
			hash := sha256.Sum256([]byte(token))
			require.Equal(t, hash[:], stored)
			require.Equal(t, expires, storedExpires)
			for range 2 {
				got, err := documentdownloads.Resolve(ctx, f.db, token, resource)
				require.NoError(t, err)
				require.Equal(t, &documentdownloads.Download{OrganizationID: f.doc.OrganizationID, DocumentID: f.doc.ExternalID}, got)
				data, err := json.Marshal(got)
				require.NoError(t, err)
				require.JSONEq(t, `{}`, string(data))
			}
			got, err := documentdownloads.Resolve(ctx, f.db, token, resource+"/other")
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}
}

func TestCreateRejectsInvalidPrincipalsAndDocuments(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"nil options", "no principal", "both principals", "negative key", "invalid hash", "missing resource", "wrong key organization", "wrong oauth organization", "wrong document organization", "missing document", "uploading", "key no read", "oauth no read", "revoked key", "revoked oauth", "deleted organization"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			opts := f.options("api")
			orgID, docID := f.doc.OrganizationID, f.doc.ID
			switch mode {
			case "nil options":
				opts = nil
			case "no principal":
				opts.APIKeyID = 0
			case "both principals":
				opts.OAuthAccessHash = f.options("oauth").OAuthAccessHash
			case "negative key":
				opts.APIKeyID = -1
			case "invalid hash":
				opts = f.options("oauth")
				opts.OAuthAccessHash = []byte("short")
			case "missing resource":
				opts.Resource = ""
			case "wrong key organization", "wrong oauth organization":
				orgID = testutil.CreateTestOrg(t, f.db, "other", "Other")
				if mode == "wrong oauth organization" {
					opts = f.options("oauth")
				}
			case "wrong document organization":
				other := testutil.CreateTestOrg(t, f.db, "other", "Other")
				doc, err := documents.Create(ctx, f.db, other, &documents.CreateOptions{Filename: "other.pdf", ContentType: "application/pdf"})
				require.NoError(t, err)
				docID = doc.ID
			case "missing document":
				docID = -1
			case "uploading":
				_, err := f.db.Exec(ctx, `UPDATE documents SET status=$1 WHERE id=$2`, enums.DocumentStatusUploading, f.doc.ID)
				require.NoError(t, err)
			case "key no read":
				_, err := f.db.Exec(ctx, `UPDATE api_keys SET scopes='{write}' WHERE id=$1`, f.key.ID)
				require.NoError(t, err)
			case "oauth no read":
				opts = f.options("oauth")
				_, err := f.db.Exec(ctx, `UPDATE mcp_oauth_tokens SET scope='write' WHERE access_hash=$1`, opts.OAuthAccessHash)
				require.NoError(t, err)
			case "revoked key":
				_, err := apikeys.RevokeByExternalID(ctx, f.db, orgID, f.key.ExternalID)
				require.NoError(t, err)
			case "revoked oauth":
				opts = f.options("oauth")
				require.NoError(t, oauth.Revoke(ctx, f.db, f.client.ID, f.tokens.AccessToken))
			case "deleted organization":
				_, err := f.db.Exec(ctx, `UPDATE organizations SET deleted_at=CURRENT_TIMESTAMP WHERE id=$1`, orgID)
				require.NoError(t, err)
			}
			token, expires, err := documentdownloads.Create(ctx, f.db, orgID, docID, opts)
			require.ErrorIs(t, err, documentdownloads.ErrInvalidDownload)
			require.Empty(t, token)
			require.True(t, expires.IsZero())
			var count int
			require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM document_downloads`).Scan(&count))
			require.Zero(t, count)
		})
	}
}

func TestResolveRevalidatesAccess(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"ticket expired", "key revoked", "key scope removed", "oauth revoked", "oauth expired", "oauth scope removed", "oauth member removed", "oauth user removed", "organization deleted", "document not uploaded"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := setup(t)
			ctx := t.Context()
			kind := "api"
			if strings.HasPrefix(mode, "oauth") {
				kind = "oauth"
			}
			opts := f.options(kind)
			token, _, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, opts)
			require.NoError(t, err)
			switch mode {
			case "key revoked":
				_, err = apikeys.RevokeByExternalID(ctx, f.db, f.doc.OrganizationID, f.key.ExternalID)
			case "oauth revoked":
				err = oauth.Revoke(ctx, f.db, f.client.ID, f.tokens.AccessToken)
			default:
				queries := map[string]string{
					"ticket expired":        `UPDATE document_downloads SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'`,
					"key scope removed":     `UPDATE api_keys SET scopes='{write}'`,
					"oauth expired":         `UPDATE mcp_oauth_tokens SET access_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'`,
					"oauth scope removed":   `UPDATE mcp_oauth_tokens SET scope='write'`,
					"oauth member removed":  `UPDATE org_members SET deleted_at=CURRENT_TIMESTAMP`,
					"oauth user removed":    `UPDATE users SET deleted_at=CURRENT_TIMESTAMP`,
					"organization deleted":  `UPDATE organizations SET deleted_at=CURRENT_TIMESTAMP`,
					"document not uploaded": `UPDATE documents SET status='uploading'`,
				}
				_, err = f.db.Exec(ctx, queries[mode])
			}
			require.NoError(t, err)
			got, err := documentdownloads.Resolve(ctx, f.db, token, resource)
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}
}

func TestDownloadUsesExactOAuthAccessToken(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	opts := f.options("oauth")
	first, _, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, opts)
	require.NoError(t, err)
	next, err := oauth.Refresh(ctx, f.db, f.client.ID, f.tokens.RefreshToken, resource, "read")
	require.NoError(t, err)
	nextHash := sha256.Sum256([]byte(next.AccessToken))
	second, _, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, &documentdownloads.CreateOptions{OAuthAccessHash: nextHash[:], Resource: resource})
	require.NoError(t, err)
	_, err = f.db.Exec(ctx, `UPDATE mcp_oauth_tokens SET access_expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second' WHERE access_hash=$1`, opts.OAuthAccessHash)
	require.NoError(t, err)
	got, err := documentdownloads.Resolve(ctx, f.db, first, resource)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = documentdownloads.Resolve(ctx, f.db, second, resource)
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestCreateRemovesOnlyExpiredDownloads(t *testing.T) {
	t.Parallel()
	f := setup(t)
	ctx := t.Context()
	first, _, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, f.options("api"))
	require.NoError(t, err)
	_, err = f.db.Exec(ctx, `UPDATE document_downloads SET expires_at=CURRENT_TIMESTAMP-INTERVAL '1 second'`)
	require.NoError(t, err)
	second, _, err := documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, f.options("api"))
	require.NoError(t, err)
	_, _, err = documentdownloads.Create(ctx, f.db, f.doc.OrganizationID, f.doc.ID, f.options("api"))
	require.NoError(t, err)
	var count int
	require.NoError(t, f.db.QueryRow(ctx, `SELECT count(*) FROM document_downloads`).Scan(&count))
	require.Equal(t, 2, count)
	got, err := documentdownloads.Resolve(ctx, f.db, first, resource)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = documentdownloads.Resolve(ctx, f.db, second, resource)
	require.NoError(t, err)
	require.NotNil(t, got)
}

func TestResolveRejectsMalformedTokensWithoutDatabase(t *testing.T) {
	t.Parallel()
	for _, token := range []string{"", "secret", "mcp_at_" + strings.Repeat("A", 43), "mcp_dl_short", "mcp_dl_" + strings.Repeat("!", 43), "mcp_dl_" + strings.Repeat("A", 42) + "B"} {
		got, err := documentdownloads.Resolve(t.Context(), nil, token, resource)
		require.NoError(t, err)
		require.Nil(t, got)
	}
}
