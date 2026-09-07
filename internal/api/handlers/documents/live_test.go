package documents_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/handlers/documents"
	"nautilus/internal/api/version"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestUploadMiniStack(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set S3_TEST_ENDPOINT to the local initialized MiniStack service")
	}
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "upload-live", "Upload")
	org, err := organizations.Get(ctx, db, orgID)
	require.NoError(t, err)
	_, token, err := apikeys.Create(ctx, db, orgID, userID, &apikeys.CreateOptions{
		Name: "upload", Scopes: []apikeys.Scope{apikeys.ScopeRead, apikeys.ScopeWrite},
	})
	require.NoError(t, err)
	store := s3store.New(aws.Config{
		Region: "us-east-1", BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, "nautilus-dev", true)
	keys := liveKeys{}
	router := mux.New(mux.Config{Middleware: []mux.Middleware{
		authentication.RequireAPIKey(db), middleware.OrganizationEncryption(keys), version.Middleware,
	}})
	documents.Mount(router, db, store)
	data := []byte("%PDF-1.7\nsynthetic mail content for upload verification\n")
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	file, err := writer.CreateFormFile("file", "synthetic-letter.pdf")
	require.NoError(t, err)
	_, err = file.Write(data)
	require.NoError(t, err)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/documents", &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)
	var response struct {
		Document struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
		} `json:"document"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "synthetic-letter.pdf", response.Document.Filename)
	require.Equal(t, int64(len(data)), response.Document.Size)
	var key string
	require.NoError(t, db.QueryRow(ctx, "SELECT object_key FROM documents WHERE organization_id = $1 AND external_id = $2 AND status = 'ready'", orgID, response.Document.ID).Scan(&key))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		require.NoError(t, store.Delete(cleanupCtx, key))
	})
	object, err := store.Get(ctx, key, nil)
	require.NoError(t, err)
	envelope, err := io.ReadAll(object.Body)
	require.NoError(t, object.Body.Close())
	require.NoError(t, err)
	require.False(t, bytes.Contains(envelope, data))
	require.Equal(t, "application/octet-stream", object.ContentType)
	require.Empty(t, object.Metadata)
	plaintext, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, envelope, encrypt.Binding{
		Purpose: "document", RecordID: response.Document.ID,
	})
	require.NoError(t, err)
	require.Equal(t, data, plaintext)
	clear(plaintext)
	otherID := testutil.CreateTestOrg(t, db, "upload-other", "Other")
	_, otherToken, err := apikeys.Create(ctx, db, otherID, userID, &apikeys.CreateOptions{Name: "read", Scopes: []apikeys.Scope{apikeys.ScopeRead}})
	require.NoError(t, err)
	for _, tt := range []struct {
		token  string
		status int
	}{{token, http.StatusOK}, {otherToken, http.StatusNotFound}} {
		req := httptest.NewRequest(http.MethodGet, "/documents/"+response.Document.ID, nil)
		req.Header.Set("Authorization", "Bearer "+tt.token)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tt.status, rec.Code)
		require.NotContains(t, rec.Body.String(), key)
	}
}

type liveKeys struct{}

func (liveKeys) OrganizationKey(_ context.Context, id string) ([]byte, error) {
	key := sha256.Sum256([]byte(id))
	return key[:], nil
}

func (liveKeys) UserKey(context.Context) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 32), nil
}

func TestUploadAPIRequiresWriteScope(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		scope  apikeys.Scope
		status int
		code   string
	}{
		{apikeys.ScopeRead, http.StatusForbidden, "APIKEY-10"},
		{apikeys.ScopeWrite, http.StatusServiceUnavailable, "DOC-08"},
	} {
		t.Run(string(tt.scope), func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			userID := testutil.CreateTestUser(t, db, nil)
			orgID := testutil.CreateTestOrg(t, db, "api-upload-scope", "Scope")
			router := mux.New(mux.Config{Middleware: []mux.Middleware{
				authentication.RequireAPIKey(db), middleware.OrganizationEncryption(liveKeys{}), version.Middleware,
			}})
			documents.Mount(router, db, nil)
			_, token, err := apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{Name: string(tt.scope), Scopes: []apikeys.Scope{tt.scope}})
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/documents", nil)
			req.Header.Set("Authorization", "Bearer "+token)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			require.Contains(t, rec.Body.String(), tt.code)
		})
	}
}
