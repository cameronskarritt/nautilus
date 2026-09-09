package upload_test

import (
	"context"
	"strings"
	"testing"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/embedding"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/search"
	"nautilus/internal/search/hybrid"
	"nautilus/internal/temporal/failure"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestIndexUpload(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"success", "different organization", "deleted organization", "deleted during read", "not uploaded", "wrong purpose", "wrong record", "wrong key", "oversized object", "provider failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, "index", "Index", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "private-invoice.pdf", ContentType: "application/pdf", Size: 10})
			require.NoError(t, err)
			if name != "not uploaded" {
				_, err = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
				require.NoError(t, err)
			}
			keys := indexKeys{ocrKeys: ocrKeys{}}
			enc := encrypt.ForOrganization(keys, org.ExternalID)
			binding := encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID}
			if name == "wrong purpose" {
				binding.Purpose = "document"
			}
			if name == "wrong record" {
				binding.RecordID = uuid.New().String()
			}
			if name == "wrong key" {
				enc = encrypt.ForOrganization(keys, uuid.New().String())
			}
			ciphertext, err := enc.Seal(t.Context(), []byte("private extracted phrase"), binding)
			require.NoError(t, err)
			store := &ocrStore{data: ciphertext}
			indexer := &testIndexer{}
			input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
			switch name {
			case "different organization":
				input.OrganizationID++
			case "deleted organization":
				require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
			case "deleted during read":
				keys.beforeKey = func() { require.NoError(t, organizations.Delete(t.Context(), db, org.ID)) }
			case "oversized object":
				store.data = make([]byte, encrypt.MaxPlaintextSize+128<<10)
			case "provider failure":
				indexer.err = errors.New("private extracted phrase, provider credentials")
			}
			a := upload.Activities{DB: db, Store: store, Keys: keys, Indexer: indexer}
			err = a.Index(t.Context(), input)
			if name == "success" {
				require.NoError(t, err)
				require.Equal(t, doc.ObjectKey+"/ocr", store.getKey)
				require.Equal(t, org.ExternalID, indexer.orgID)
				require.Equal(t, search.Document{ID: doc.ExternalID, Text: doc.Filename + "\nprivate extracted phrase"}, indexer.doc)
				store.read = 0
				require.NoError(t, a.Index(t.Context(), input))
				require.Equal(t, 2, indexer.calls)
				require.Equal(t, doc.ExternalID, indexer.doc.ID)
			} else {
				var appErr *temporal.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.NotContains(t, err.Error(), "private")
				require.NotContains(t, err.Error(), "credentials")
				require.Nil(t, appErr.Unwrap())
				if name != "provider failure" {
					require.Zero(t, indexer.calls)
				} else {
					require.False(t, appErr.NonRetryable())
					got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
					require.NoError(t, err)
					require.Equal(t, "uploaded", got.Status.String())
				}
				if name == "different organization" || name == "deleted organization" || name == "not uploaded" {
					require.Zero(t, store.gets)
					require.True(t, appErr.NonRetryable())
				}
				if name == "deleted during read" {
					require.True(t, appErr.NonRetryable())
				}
				if name == "oversized object" {
					require.Less(t, store.read, len(store.data))
				}
			}
			if store.gets > 0 {
				require.True(t, store.closed)
			}
		})
	}
}

func TestWorkflowIndexRetries(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"retry", "terminal", "OCR-only history"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			env.SetFailureConverter(failure.NewConverter())
			upload.Register(env, upload.Activities{})
			input := upload.Input{OrganizationID: 1, DocumentID: uuid.New().String()}
			var order []string
			env.OnActivity("FinalizeUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error { order = append(order, "finalize"); return nil }).Once()
			env.OnActivity("UploadWebhookDeliveries", mock.Anything, input).Return([]string{}, nil).Once()
			env.OnActivity("OCRUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error { order = append(order, "ocr"); return nil }).Once()
			if name == "OCR-only history" {
				env.OnGetVersion("upload-index", workflow.DefaultVersion, workflow.Version(1)).Return(workflow.DefaultVersion).Once()
			} else {
				env.OnActivity("IndexUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error {
					require.Equal(t, []string{"finalize", "ocr"}, order)
					order = append(order, "index")
					if name == "terminal" {
						return failure.New("unavailable", "IndexUnavailable", true)
					}
					return errors.New("temporary failure")
				}).Once()
				if name == "retry" {
					env.OnActivity("IndexUpload", mock.Anything, input).Return(nil).Once()
				}
			}
			env.ExecuteWorkflow(upload.Name, input)
			if name == "terminal" {
				require.Error(t, env.GetWorkflowError())
			} else {
				require.NoError(t, env.GetWorkflowError())
			}
			env.AssertExpectations(t)
		})
	}
}

type testIndexer struct {
	search.Indexer
	orgID string
	doc   search.Document
	calls int
	err   error
}

func (i *testIndexer) Index(_ context.Context, orgID string, doc *search.Document) error {
	i.calls++
	i.orgID, i.doc = orgID, *doc
	return i.err
}

type indexKeys struct {
	ocrKeys
	beforeKey func()
}

func (k indexKeys) OrganizationKey(ctx context.Context, id string) ([]byte, error) {
	if k.beforeKey != nil {
		k.beforeKey()
	}
	return k.ocrKeys.OrganizationKey(ctx, id)
}

func TestIndexHybridFailures(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"chunk budget", "embedding unavailable", "store unavailable"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, "hybrid", "Hybrid", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "private.pdf", ContentType: "application/pdf", Size: 10})
			require.NoError(t, err)
			_, err = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			text := "private extracted phrase"
			provider := &indexProvider{}
			switch name {
			case "chunk budget":
				// Valid OCR output at the hybrid budget exceeds it once the filename is included.
				text = strings.Repeat("x", 491776)
			case "embedding unavailable":
				provider.embedErr = errors.New("private provider credentials")
			case "store unavailable":
				provider.storeErr = errors.New("private provider credentials")
			}
			keys := ocrKeys{}
			ciphertext, err := encrypt.ForOrganization(keys, org.ExternalID).Seal(t.Context(), []byte(text), encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
			require.NoError(t, err)
			store := &ocrStore{data: ciphertext}
			indexer, err := hybrid.New(provider, provider, nil)
			require.NoError(t, err)
			a := upload.Activities{DB: db, Store: store, Keys: keys, Indexer: indexer}
			input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
			err = a.Index(t.Context(), input)
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, err, &appErr)
			require.Equal(t, name == "chunk budget", appErr.NonRetryable())
			require.NotContains(t, err.Error(), "private")
			require.Nil(t, appErr.Unwrap())
			require.True(t, store.closed)
			if name == "chunk budget" {
				require.Equal(t, "IndexUnavailable", appErr.Type())
				require.Zero(t, provider.embeds)
				require.Zero(t, provider.replaces)
				return
			}
			require.Equal(t, "IndexFailed", appErr.Type())
			require.Equal(t, 1, provider.embeds)
			if name == "embedding unavailable" {
				require.Zero(t, provider.replaces)
			} else {
				require.Equal(t, 1, provider.replaces)
			}
			provider.embedErr, provider.storeErr = nil, nil
			store.read = 0
			require.NoError(t, a.Index(t.Context(), input))
			require.Equal(t, doc.ExternalID, provider.docID)
		})
	}
}

type indexProvider struct {
	search.VectorStore
	embedding.Embedder
	embeds, replaces   int
	embedErr, storeErr error
	docID              string
}

func (*indexProvider) Model() embedding.Model {
	return embedding.Model{Name: "test", Dimensions: 1}
}

func (p *indexProvider) Embed(_ context.Context, texts []string, _ *embedding.Options) ([][]float32, error) {
	p.embeds++
	if p.embedErr != nil {
		return nil, p.embedErr
	}
	vectors := make([][]float32, len(texts))
	for i := range vectors {
		vectors[i] = []float32{1}
	}
	return vectors, nil
}

func (p *indexProvider) Replace(_ context.Context, _, docID string, _ []search.Chunk) error {
	p.replaces++
	p.docID = docID
	return p.storeErr
}
