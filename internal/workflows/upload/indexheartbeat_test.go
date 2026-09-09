package upload_test

import (
	"context"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/search"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestIndexWorkerShutdown(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org, err := organizations.Create(t.Context(), db, "index-shutdown", "Index", false, optional.Empty[organizations.Settings]())
	require.NoError(t, err)
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Size: 1})
	require.NoError(t, err)
	_, err = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	keys := ocrKeys{}
	ciphertext, err := encrypt.ForOrganization(keys, org.ExternalID).Seal(t.Context(), []byte("private document text"), encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestActivityEnvironment()
	env.SetTestTimeout(5 * time.Second)
	stop := make(chan struct{})
	env.SetWorkerStopChannel(stop)
	beat := make(chan bool, 1)
	env.SetOnActivityHeartbeatListener(func(_ *activity.Info, details converter.EncodedValues) {
		select {
		case beat <- details.HasValues():
		default:
		}
	})
	indexer := shutdownIndexer{t: t, beat: beat, stop: stop}
	a := upload.Activities{DB: db, Store: &ocrStore{data: ciphertext}, Keys: keys, Indexer: indexer}
	env.RegisterActivity(a.Index)
	_, err = env.ExecuteActivity(a.Index, upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID})
	require.Error(t, err)
	require.NotContains(t, err.Error(), "private document text")
}

type shutdownIndexer struct {
	search.Indexer
	t    *testing.T
	beat <-chan bool
	stop chan struct{}
}

func (i shutdownIndexer) Index(ctx context.Context, _ string, _ *search.Document) error {
	select {
	case details := <-i.beat:
		require.False(i.t, details, "heartbeats must not contain document data")
	case <-time.After(2 * time.Second):
		i.t.Fatal("indexing did not heartbeat")
	}
	close(i.stop)
	select {
	case <-ctx.Done():
		return errors.Wrap(ctx.Err(), "indexing canceled")
	case <-time.After(2 * time.Second):
		i.t.Fatal("indexing did not stop with its worker")
		return nil
	}
}
