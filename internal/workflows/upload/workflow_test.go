package upload_test

import (
	"context"
	"strings"
	"testing"
	"time"
	"uuid"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/api/enums/v1"
	"go.temporal.io/api/serviceerror"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/mocks"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/testsuite"

	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestWorkflow(t *testing.T) {
	t.Parallel()
	for _, terminal := range []bool{false, true} {
		t.Run(map[bool]string{false: "retries transient failure", true: "stops on terminal failure"}[terminal], func(t *testing.T) {
			t.Parallel()
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, nil)
			input := upload.Input{OrganizationID: 1, DocumentID: uuid.New().String()}
			if terminal {
				env.OnActivity("FinalizeUpload", mock.Anything, input).Return(temporal.NewNonRetryableApplicationError("unavailable", "UploadUnavailable", nil)).Once()
			} else {
				env.OnActivity("FinalizeUpload", mock.Anything, input).Return(errors.New("temporary failure")).Once()
				env.OnActivity("FinalizeUpload", mock.Anything, input).Return(nil).Once()
			}
			env.ExecuteWorkflow(upload.Name, input)
			require.True(t, env.IsWorkflowCompleted())
			if terminal {
				var appErr *temporal.ApplicationError
				require.ErrorAs(t, env.GetWorkflowError(), &appErr)
				require.True(t, appErr.NonRetryable())
			} else {
				require.NoError(t, env.GetWorkflowError())
			}
			env.AssertExpectations(t)
		})
	}
}

func TestStart(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		err  error
	}{
		{name: "started"},
		{name: "already started", err: serviceerror.NewWorkflowExecutionAlreadyStarted("duplicate", "request", "run")},
		{name: "unavailable", err: serviceerror.NewUnavailable("unavailable")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := mocks.NewClient(t)
			input := upload.Input{OrganizationID: 42, DocumentID: uuid.New().String()}
			c.On("ExecuteWorkflow", mock.MatchedBy(func(ctx context.Context) bool {
				deadline, ok := ctx.Deadline()
				return ok && time.Until(deadline) > 0 && time.Until(deadline) <= 10*time.Second
			}), client.StartWorkflowOptions{
				ID:                                       "upload-42-" + input.DocumentID,
				TaskQueue:                                "uploads",
				WorkflowIDReusePolicy:                    enums.WORKFLOW_ID_REUSE_POLICY_REJECT_DUPLICATE,
				WorkflowExecutionErrorWhenAlreadyStarted: true,
			}, upload.Name, input).Return(nil, tt.err).Once()
			input.DocumentID = strings.ToUpper(input.DocumentID)
			err := upload.Start(t.Context(), c, input)
			if tt.name == "unavailable" {
				require.ErrorIs(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestInvalidInput(t *testing.T) {
	t.Parallel()
	for _, input := range []upload.Input{
		{OrganizationID: 0, DocumentID: uuid.New().String()},
		{OrganizationID: -1, DocumentID: uuid.New().String()},
		{OrganizationID: 1},
		{OrganizationID: 1, DocumentID: "malformed"},
	} {
		t.Run(input.DocumentID, func(t *testing.T) {
			t.Parallel()
			require.Error(t, upload.Start(t.Context(), nil, input))
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, nil)
			env.ExecuteWorkflow(upload.Name, input)
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, env.GetWorkflowError(), &appErr)
			require.True(t, appErr.NonRetryable())
		})
	}
}

func TestFinalizeUpload(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org, err := organizations.Create(t.Context(), db, t.Name(), "upload-workflow", false, optional.Empty[organizations.Settings]())
	require.NoError(t, err)
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "private.txt", ContentType: "text/plain", Size: 10})
	require.NoError(t, err)
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	upload.Register(env, db)
	input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
	env.ExecuteWorkflow(upload.Name, input)
	require.NoError(t, env.GetWorkflowError())
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, "uploaded", got.Status.String())

	env = suite.NewTestWorkflowEnvironment()
	upload.Register(env, db)
	env.ExecuteWorkflow(upload.Name, input)
	require.NoError(t, env.GetWorkflowError())

}

func TestFinalizeUploadUnavailable(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"missing", "failed", "deleted organization", "different organization"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, t.Name(), "unavailable-upload", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "private.txt", ContentType: "text/plain", Size: 10})
			require.NoError(t, err)
			input := upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}
			switch state {
			case "missing":
				input.DocumentID = uuid.New().String()
			case "failed":
				require.NoError(t, documents.MarkFailed(t.Context(), db, org.ID, doc.ExternalID))
			case "deleted organization":
				require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
			case "different organization":
				input.OrganizationID++
			}
			var suite testsuite.WorkflowTestSuite
			env := suite.NewTestWorkflowEnvironment()
			upload.Register(env, db)
			env.ExecuteWorkflow(upload.Name, input)
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, env.GetWorkflowError(), &appErr)
			require.True(t, appErr.NonRetryable())
			require.Equal(t, "UploadUnavailable", appErr.Type())
		})
	}
}
