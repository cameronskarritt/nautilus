package documents_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
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
	"golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/durationpb"

	"nautilus/internal/app/handlers/documents"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore/s3store"
	"nautilus/internal/ocr"
	"nautilus/internal/ocr/lmstudio"
	"nautilus/internal/ocr/stub"
	"nautilus/internal/search/opensearch"
	"nautilus/internal/temporal"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestUploadMiniStack(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	address := os.Getenv("TEMPORAL_TEST_ADDRESS")
	searchURL := os.Getenv("OPENSEARCH_TEST_URL")
	ocrURL := os.Getenv("LMSTUDIO_OCR_TEST_URL")
	if endpoint == "" || address == "" || searchURL == "" {
		t.Skip("set S3_TEST_ENDPOINT, TEMPORAL_TEST_ADDRESS, and OPENSEARCH_TEST_URL to local test services")
	}
	db := testutil.SetupTestDBWithCommit(t)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	namespace := "upload-test-" + uuid.New().String()
	indexer, err := opensearch.New(opensearch.Config{URL: searchURL, Index: namespace})
	require.NoError(t, err)
	require.NoError(t, indexer.EnsureIndex(ctx))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(cleanupCtx, http.MethodDelete, searchURL+"/"+namespace, nil)
		require.NoError(t, err)
		resp, err := http.DefaultClient.Do(req)
		require.NoError(t, err)
		require.NoError(t, resp.Body.Close())
		require.Equal(t, http.StatusOK, resp.StatusCode)
	})
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
	ctx = sessions.WithContext(users.WithContext(ctx, &users.User{ID: userID, Admin: true}), 1)
	store := s3store.New(aws.Config{
		Region: "us-east-1", BaseEndpoint: aws.String(endpoint),
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, "nautilus-dev", true)
	keys := liveKeys{}
	router := mux.New(mux.Config{})
	documents.NewMux(db, store, c).MountAdmin(router, "/admin/organizations/{orgID:<uuid>}/documents", keys)
	path := "/admin/organizations/" + org.ExternalID + "/documents"
	data := syntheticScan(t)
	var extractor ocr.OCR = stub.OCR{}
	query := "synthetic"
	if ocrURL != "" {
		extractor, err = lmstudio.New(lmstudio.Config{URL: ocrURL, Model: "allenai/olmocr-2-7b"})
		require.NoError(t, err)
		query = "marmalade"
	}
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for range 2 {
		file, err := writer.CreateFormFile("file", "synthetic-letter.png")
		require.NoError(t, err)
		_, err = file.Write(data)
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, path, &body).WithContext(ctx)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusAccepted, rec.Code)
	var response struct {
		Document struct {
			ID        string `json:"id"`
			Filename  string `json:"filename"`
			Size      int64  `json:"size"`
			PageCount int    `json:"page_count"`
			Status    string `json:"status"`
		} `json:"document"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Equal(t, "synthetic-letter.pdf", response.Document.Filename)
	require.Zero(t, response.Document.Size)
	require.Equal(t, 2, response.Document.PageCount)
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
	var key, pdfKey string
	require.NoError(t, db.QueryRow(ctx, "SELECT object_key FROM documents WHERE organization_id = $1 AND external_id = $2 AND status = 'uploading'", orgID, response.Document.ID).Scan(&key))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if pdfKey != "" {
			require.NoError(t, store.Delete(cleanupCtx, pdfKey))
		}
		require.NoError(t, store.Delete(cleanupCtx, key+"/ocr"))
		require.NoError(t, store.Delete(cleanupCtx, key+"/pages/1"))
		require.NoError(t, store.Delete(cleanupCtx, key+"/pages/2"))
	})
	object, err := store.Get(ctx, key+"/pages/1", nil)
	require.NoError(t, err)
	envelope, err := io.ReadAll(object.Body)
	require.NoError(t, object.Body.Close())
	require.NoError(t, err)
	require.False(t, bytes.Contains(envelope, data))
	require.Equal(t, "application/octet-stream", object.ContentType)
	require.Empty(t, object.Metadata)
	plaintext, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, envelope, encrypt.Binding{
		Purpose: "document-page", RecordID: response.Document.ID + "/1",
	})
	require.NoError(t, err)
	require.Equal(t, data, plaintext)
	clear(plaintext)

	// Source images are encrypted before any worker runs; PDF publication is asynchronous.
	w := temporal.NewWorker(c, enums.QueueUploads)
	upload.Register(w, upload.Activities{DB: db, Store: store, Keys: keys, OCR: extractor, Indexer: indexer})
	require.NoError(t, w.Start())
	t.Cleanup(w.Stop)
	require.NoError(t, c.GetWorkflow(ctx, workflowID, "").Get(ctx, nil))
	require.NoError(t, db.QueryRow(ctx, "SELECT pdf_key FROM documents WHERE organization_id = $1 AND external_id = $2", orgID, response.Document.ID).Scan(&pdfKey))
	require.NotEmpty(t, pdfKey)
	hits, err := indexer.Search(ctx, org.ExternalID, query, nil)
	require.NoError(t, err)
	require.Equal(t, []string{response.Document.ID}, hits)
	hits, err = indexer.Search(ctx, uuid.New().String(), query, nil)
	require.NoError(t, err)
	require.Empty(t, hits)
	artifact, err := store.Get(ctx, key+"/ocr", nil)
	require.NoError(t, err)
	output, err := io.ReadAll(artifact.Body)
	require.NoError(t, artifact.Body.Close())
	require.NoError(t, err)
	require.Equal(t, "application/octet-stream", artifact.ContentType)
	require.Empty(t, artifact.Metadata)
	extracted, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, output, encrypt.Binding{Purpose: "document-ocr", RecordID: response.Document.ID})
	require.NoError(t, err)
	if ocrURL == "" {
		require.Equal(t, "\n", string(extracted))
	} else {
		require.Contains(t, string(extracted), "Marmalade")
		require.Contains(t, string(extracted), "$185.40")
		require.NotContains(t, string(extracted), "primary_language")
		require.NotContains(t, string(output), "Marmalade")
	}
	clear(extracted)
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
		require.NotContains(t, string(encoded), "Marmalade")
		if attrs := event.GetWorkflowExecutionStartedEventAttributes(); attrs != nil {
			require.Len(t, attrs.Input.Payloads, 1)
			require.JSONEq(t, `{"organization_id":`+strconv.Itoa(orgID)+`,"document_id":"`+response.Document.ID+`"}`, string(attrs.Input.Payloads[0].Data))
		}
		if attrs := event.GetActivityTaskCompletedEventAttributes(); attrs != nil {
			require.Empty(t, attrs.Result.GetPayloads())
		}
		if attrs := event.GetActivityTaskScheduledEventAttributes(); attrs != nil {
			require.Len(t, attrs.Input.Payloads, 1)
			require.JSONEq(t, `{"organization_id":`+strconv.Itoa(orgID)+`,"document_id":"`+response.Document.ID+`"}`, string(attrs.Input.Payloads[0].Data))
		}
	}
	req = httptest.NewRequest(http.MethodGet, path+"/"+response.Document.ID+"/content", nil).WithContext(ctx)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, bytes.HasPrefix(rec.Body.Bytes(), []byte("%PDF-")))
	require.Equal(t, "application/pdf", rec.Header().Get("Content-Type"))
	require.Contains(t, rec.Header().Get("Content-Disposition"), "synthetic-letter.pdf")
	var pdfSize int64
	require.NoError(t, db.QueryRow(ctx, "SELECT size FROM documents WHERE organization_id = $1 AND external_id = $2", orgID, response.Document.ID).Scan(&pdfSize))
	require.Equal(t, pdfSize, int64(rec.Body.Len()))
	otherID := testutil.CreateTestOrg(t, db, "upload-other", "Other")
	other, err := organizations.Get(ctx, db, otherID)
	require.NoError(t, err)
	for _, tt := range []struct {
		organizationID string
		status         int
	}{{org.ExternalID, http.StatusOK}, {other.ExternalID, http.StatusNotFound}} {
		req := httptest.NewRequest(http.MethodGet, "/admin/organizations/"+tt.organizationID+"/documents/"+response.Document.ID, nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		require.Equal(t, tt.status, rec.Code)
		require.NotContains(t, rec.Body.String(), key)
	}

}

func syntheticScan(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 640, 220))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	d := font.Drawer{Dst: img, Src: image.NewUniform(color.Black), Face: basicfont.Face7x13, Dot: fixed.P(35, 65)}
	d.DrawString("Marmalade invoice")
	d.Dot = fixed.P(35, 100)
	d.DrawString("Amount due: $185.40")
	large := image.NewRGBA(image.Rect(0, 0, 2560, 880))
	draw.NearestNeighbor.Scale(large, large.Bounds(), img, img.Bounds(), draw.Src, nil)
	var b bytes.Buffer
	require.NoError(t, png.Encode(&b, large))
	return b.Bytes()
}

type liveKeys struct{}

func (liveKeys) OrganizationKey(_ context.Context, id string) ([]byte, error) {
	key := sha256.Sum256([]byte(id))
	return key[:], nil
}

func (liveKeys) UserKey(context.Context) ([]byte, error) {
	return bytes.Repeat([]byte{1}, 32), nil
}
