package upload

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/api/workflowservice/v1"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/workflow"

	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestRecoverUpload(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"complete without ready marker", "legacy complete", "legacy missing", "partial", "accepted", "lookup unavailable", "storage unavailable", "start timeout retry", "ready missing sources", "expired recovery"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Recovery")
			doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: 123}}})
			require.NoError(t, err)
			if name == "legacy complete" || name == "legacy missing" {
				_, err := db.Exec(t.Context(), `DELETE FROM document_pages WHERE document_id = $1`, doc.ID)
				require.NoError(t, err)
				_, err = db.Exec(t.Context(), `UPDATE documents SET page_count = 0 WHERE id = $1`, doc.ID)
				require.NoError(t, err)
				doc.PageCount = 0
			}
			c := mocks.NewClient(t)
			store := &recoveryStore{}
			a := Activities{DB: db, Store: store}
			input := Input{OrganizationID: orgID, DocumentID: doc.ExternalID}
			lookupErr := serviceerror.NewNotFound("missing")
			if name == "accepted" {
				lookupErr = nil
			}
			if name == "lookup unavailable" {
				lookupErr = serviceerror.NewUnavailable("offline")
			}
			c.On("DescribeWorkflowExecution", mock.Anything, workflowID(input), "").Return(&workflowservice.DescribeWorkflowExecutionResponse{}, lookupErr)
			if name == "partial" || name == "legacy missing" || name == "ready missing sources" {
				store.err = objectstore.ErrNotFound
			}
			if name == "storage unavailable" {
				store.err = errors.New("offline")
			}
			if name == "ready missing sources" {
				ready, err := documents.ReadyUpload(t.Context(), db, doc)
				require.NoError(t, err)
				require.True(t, ready)
				doc.UploadReady = true
			}
			if name == "expired recovery" {
				_, err := db.Exec(t.Context(), `UPDATE documents SET upload_token = uuid_generate_v4() WHERE id = $1`, doc.ID)
				require.NoError(t, err)
			}
			if name == "complete without ready marker" || name == "legacy complete" || name == "ready missing sources" || name == "start timeout retry" {
				options := mock.MatchedBy(func(opts client.StartWorkflowOptions) bool { return opts.ID == workflowID(input) })
				if name == "start timeout retry" {
					c.On("ExecuteWorkflow", mock.Anything, options, Name, input).Return(nil, context.DeadlineExceeded).Once()
				} else {
					c.On("ExecuteWorkflow", mock.Anything, options, Name, input).Return(nil, nil).Once()
				}
			}
			err = a.recoverUpload(t.Context(), c, doc)
			if name == "lookup unavailable" || name == "storage unavailable" || name == "start timeout retry" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			got, err := documents.GetByExternalID(t.Context(), db, orgID, doc.ExternalID)
			require.NoError(t, err)
			status := enums.DocumentStatusUploading
			if name == "partial" || name == "legacy missing" {
				status = enums.DocumentStatusFailed
			}
			require.Equal(t, status, got.Status)
			if name == "accepted" || name == "lookup unavailable" || name == "ready missing sources" {
				require.Zero(t, store.heads)
			}
			if name == "start timeout retry" {
				require.True(t, got.UploadReady)
				c.On("ExecuteWorkflow", mock.Anything, mock.Anything, Name, input).Return(nil, serviceerror.NewWorkflowExecutionAlreadyStarted("accepted", "", "run")).Once()
				require.NoError(t, a.recoverUpload(t.Context(), c, got))
			}
		})
	}
}

type recoveryStore struct {
	objectstore.Store
	err   error
	heads int
}

func (s *recoveryStore) Head(context.Context, string) (*objectstore.ObjectInfo, error) {
	s.heads++
	return &objectstore.ObjectInfo{Size: 200}, s.err
}

func TestRecoveryWorkflowContinuesCursor(t *testing.T) {
	t.Parallel()
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	Register(env, Activities{})
	start := time.Now()
	env.SetStartTime(start)
	env.OnActivity("RecoverUploads", mock.Anything, 41).Return(51, nil).Once()
	env.ExecuteWorkflow(RecoveryName, 41)
	var next *workflow.ContinueAsNewError
	require.ErrorAs(t, env.GetWorkflowError(), &next)
	require.Equal(t, RecoveryName, next.WorkflowType.Name)
	var cursor int
	require.NoError(t, converter.GetDefaultDataConverter().FromPayloads(next.Input, &cursor))
	require.Equal(t, 51, cursor)
	require.GreaterOrEqual(t, env.Now().Sub(start), time.Minute)
	env.AssertExpectations(t)
}
