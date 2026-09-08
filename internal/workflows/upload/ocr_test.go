package upload_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"io"
	"testing"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/ocr"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestOCRUpload(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"success", "different organization", "deleted organization", "not uploaded", "wrong purpose", "wrong record", "wrong key", "size mismatch", "oversized object", "invalid document", "provider failure", "store failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, "ocr", "OCR", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			data := []byte("sensitive original document")
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "secret.txt", ContentType: "text/plain", Size: int64(len(data))})
			require.NoError(t, err)
			if name != "not uploaded" {
				_, err = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
				require.NoError(t, err)
			}
			keys := ocrKeys{}
			enc := encrypt.ForOrganization(keys, org.ExternalID)
			binding := encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID}
			if name == "wrong purpose" {
				binding.Purpose = "document-ocr"
			}
			if name == "wrong record" {
				binding.RecordID = uuid.New().String()
			}
			if name == "wrong key" {
				enc = encrypt.ForOrganization(keys, uuid.New().String())
			}
			if name == "size mismatch" {
				data = append(data, '!')
			}
			ciphertext, err := enc.Seal(t.Context(), data, binding)
			require.NoError(t, err)
			store := &ocrStore{data: ciphertext}
			extractor := &testOCR{t: t, want: data}
			a := upload.Activities{DB: db, Store: store, Keys: keys, OCR: extractor}
			input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
			switch name {
			case "different organization":
				input.OrganizationID++
			case "deleted organization":
				require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
			case "oversized object":
				store.data = make([]byte, encrypt.MaxPlaintextSize+128<<10)
			case "provider failure":
				extractor.err = errors.New("sensitive provider response")
			case "invalid document":
				extractor.err = errors.Wrap(ocr.ErrInvalidDocument, "sensitive provider detail")
			case "store failure":
				store.err = errors.New("sensitive storage response")
			}
			err = a.Extract(t.Context(), input)
			if name != "success" {
				var appErr *temporal.ApplicationError
				require.ErrorAs(t, err, &appErr)
				require.NotContains(t, err.Error(), "sensitive")
				require.NotContains(t, err.Error(), doc.Filename)
				require.Nil(t, appErr.Unwrap())
				require.Empty(t, store.output)
				if name == "different organization" || name == "deleted organization" || name == "not uploaded" {
					require.Zero(t, store.gets)
					require.True(t, appErr.NonRetryable())
				}
				if name == "provider failure" || name == "store failure" {
					require.False(t, appErr.NonRetryable())
					got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
					require.NoError(t, err)
					require.Equal(t, "uploaded", got.Status.String())
				}
				if name == "oversized object" {
					require.Less(t, store.read, len(store.data))
				}
				if name == "invalid document" {
					require.True(t, appErr.NonRetryable())
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, doc.ObjectKey, store.getKey)
				require.Equal(t, doc.ObjectKey+"/ocr", store.putKey)
				require.Equal(t, "application/octet-stream", store.opts.ContentType.Data)
				require.False(t, store.opts.Metadata.IsSet())
				require.NotContains(t, string(store.output), "sensitive extracted text")
				plaintext, err := enc.Open(t.Context(), store.output, encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
				require.NoError(t, err)
				require.Equal(t, "sensitive extracted text", string(plaintext))
				clear(plaintext)
				_, err = enc.Open(t.Context(), store.output, binding)
				require.Error(t, err)
				first := bytes.Clone(store.output)
				store.read = 0
				require.NoError(t, a.Extract(t.Context(), input))
				require.Equal(t, doc.ObjectKey+"/ocr", store.putKey)
				require.NotEqual(t, first, store.output)
			}
			if store.gets > 0 {
				require.True(t, store.closed)
			}
			if extractor.retained != nil {
				_, err := extractor.retained.Seek(0, io.SeekStart)
				require.NoError(t, err)
				cleared, err := io.ReadAll(extractor.retained)
				require.NoError(t, err)
				require.Equal(t, make([]byte, len(data)), cleared)
			}
		})
	}
}

func TestWorkflowOCRRetries(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"retry", "terminal", "old history"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, upload.Activities{})
			input := upload.Input{OrganizationID: 1, DocumentID: uuid.New().String()}
			finalized := false
			env.OnActivity("FinalizeUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error { finalized = true; return nil }).Once()
			if name == "old history" {
				env.OnGetVersion("upload-ocr", workflow.DefaultVersion, workflow.Version(1)).Return(workflow.DefaultVersion).Once()
			} else {
				env.OnActivity("OCRUpload", mock.Anything, input).Return(func(context.Context, upload.Input) error {
					require.True(t, finalized)
					if name == "terminal" {
						return temporal.NewNonRetryableApplicationError("unavailable", "OCRUnavailable", nil)
					}
					return errors.New("temporary failure")
				}).Once()
				if name == "retry" {
					env.OnActivity("OCRUpload", mock.Anything, input).Return(nil).Once()
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

type ocrKeys struct{}

func (ocrKeys) OrganizationKey(_ context.Context, id string) ([]byte, error) {
	key := sha256.Sum256([]byte(id))
	return key[:], nil
}
func (ocrKeys) UserKey(context.Context) ([]byte, error) {
	return nil, errors.New("unexpected user key")
}

type testOCR struct {
	t        *testing.T
	want     []byte
	retained *bytes.Reader
	err      error
}

func (o *testOCR) Extract(_ context.Context, data io.Reader, contentType string) (string, error) {
	require.Equal(o.t, "text/plain", contentType)
	// Retain the reader's backing bytes to verify the activity clears them.
	o.retained = data.(*bytes.Reader)
	got, err := io.ReadAll(o.retained)
	require.NoError(o.t, err)
	require.Equal(o.t, o.want, got)
	return "sensitive extracted text", o.err
}

type ocrStore struct {
	objectstore.Store
	data           []byte
	output         []byte
	opts           *objectstore.PutOptions
	getKey, putKey string
	gets, read     int
	closed         bool
	err            error
}

func (s *ocrStore) Get(_ context.Context, key string, _ *objectstore.GetOptions) (*objectstore.Object, error) {
	s.getKey = key
	s.gets++
	return &objectstore.Object{Body: s}, nil
}
func (s *ocrStore) Read(p []byte) (int, error) {
	if s.read == len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.read:])
	s.read += n
	return n, nil
}
func (s *ocrStore) Close() error { s.closed = true; return nil }
func (s *ocrStore) Put(_ context.Context, key string, data io.Reader, opts *objectstore.PutOptions) error {
	if s.err != nil {
		return s.err
	}
	s.putKey, s.opts = key, opts
	var err error
	s.output, err = io.ReadAll(data)
	return errors.Wrap(err, "capture OCR artifact")
}
