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
	"strconv"
	"testing"
	"time"
	"uuid"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	temporalenums "go.temporal.io/api/enums/v1"
	"go.temporal.io/api/operatorservice/v1"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/handlers/documents"
	"nautilus/internal/api/version"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestUploadMiniStack(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	address := os.Getenv("TEMPORAL_TEST_ADDRESS")
	if endpoint == "" || address == "" {
		t.Skip("set S3_TEST_ENDPOINT and TEMPORAL_TEST_ADDRESS to local test services")
	}
	db := testutil.SetupTestDBWithCommit(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	namespace := "upload-test-" + uuid.New().String()
	namespaces, err := client.NewNamespaceClient(client.Options{HostPort: address})
	require.NoError(t, err)
	t.Cleanup(namespaces.Close)
	require.NoError(t, namespaces.Register(ctx, &workflowservice.RegisterNamespaceRequest{
		Namespace: namespace, WorkflowExecutionRetentionPeriod: durationpb.New(24 * time.Hour),
	}))
	c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: namespace})
	require.NoError(t, err)
	t.Cleanup(c.Close)
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_, err := c.OperatorService().DeleteNamespace(cleanupCtx, &operatorservice.DeleteNamespaceRequest{Namespace: namespace})
		require.NoError(t, err)
	})
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
	documents.Mount(router, db, store, c)
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
	require.Equal(t, http.StatusAccepted, rec.Code)
	var response struct {
		Document struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Size     int64  `json:"size"`
			Status   string `json:"status"`
		} `json:"document"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "synthetic-letter.pdf", response.Document.Filename)
	require.Equal(t, int64(len(data)), response.Document.Size)
	require.Equal(t, "uploading", response.Document.Status)
	workflowID := "upload-" + strconv.Itoa(orgID) + "-" + response.Document.ID
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		description, err := c.DescribeWorkflowExecution(cleanupCtx, workflowID, "")
		if err == nil && description.WorkflowExecutionInfo.Status == temporalenums.WORKFLOW_EXECUTION_STATUS_RUNNING {
			require.NoError(t, c.TerminateWorkflow(cleanupCtx, workflowID, "", "test cleanup"))
		}
	})
	var key string
	require.NoError(t, db.QueryRow(ctx, "SELECT object_key FROM documents WHERE organization_id = $1 AND external_id = $2 AND status = 'uploading'", orgID, response.Document.ID).Scan(&key))
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

	// The encrypted final object is available before any workflow worker runs.
	w := temporal.NewWorker(c, enums.QueueUploads)
	upload.Register(w, db)
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	require.NoError(t, c.GetWorkflow(ctx, workflowID, "").Get(ctx, nil))
	var status string
	require.NoError(t, db.QueryRow(ctx, "SELECT status FROM documents WHERE organization_id = $1 AND external_id = $2", orgID, response.Document.ID).Scan(&status))
	require.Equal(t, "uploaded", status)
	history := c.GetWorkflowHistory(ctx, workflowID, "", false, temporalenums.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	for history.HasNext() {
		event, err := history.Next()
		require.NoError(t, err)
		encoded, err := protojson.Marshal(event)
		require.NoError(t, err)
		require.NotContains(t, string(encoded), "synthetic-letter.pdf")
		require.NotContains(t, string(encoded), string(data))
		if attrs := event.GetWorkflowExecutionStartedEventAttributes(); attrs != nil {
			require.Len(t, attrs.Input.Payloads, 1)
			require.JSONEq(t, `{"organization_id":`+strconv.Itoa(orgID)+`,"document_id":"`+response.Document.ID+`"}`, string(attrs.Input.Payloads[0].Data))
		}
		if attrs := event.GetActivityTaskScheduledEventAttributes(); attrs != nil {
			require.Len(t, attrs.Input.Payloads, 1)
			require.JSONEq(t, `{"organization_id":`+strconv.Itoa(orgID)+`,"document_id":"`+response.Document.ID+`"}`, string(attrs.Input.Payloads[0].Data))
		}
	}
	req = httptest.NewRequest(http.MethodGet, "/documents/"+response.Document.ID+"/content", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, data, rec.Body.Bytes())
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
			documents.Mount(router, db, nil, nil)
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
