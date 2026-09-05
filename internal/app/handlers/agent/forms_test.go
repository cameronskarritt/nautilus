package agent_test

import (
	"testing"

	"nautilus/internal/app/handlers/agent"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

type messageForm interface {
	Normalize()
	Validate() error
}

func requireHTTPDetail(t *testing.T, err error, detail errors.ErrorDetail) {
	t.Helper()

	var httpErr *errors.HTTPError
	require.ErrorAs(t, err, &httpErr)
	require.Contains(t, httpErr.Errors, detail)
}

func TestForms_requireMessage(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		message     string
		expectError bool
		errorMsg    string
	}{
		{
			name:    "valid message",
			message: "hello agent",
		},
		{
			name:        "empty message",
			message:     "",
			expectError: true,
			errorMsg:    "Invalid stream request",
		},
		{
			name:        "whitespace only",
			message:     "   \t\n  ",
			expectError: true,
			errorMsg:    "Invalid stream request",
		},
	}

	forms := []struct {
		name  string
		build func(string) messageForm
	}{
		{
			name: "CreateStreamForm",
			build: func(message string) messageForm {
				return &agent.CreateStreamForm{Message: message}
			},
		},
		{
			name: "SendMessageForm",
			build: func(message string) messageForm {
				return &agent.SendMessageForm{Message: message}
			},
		},
	}

	for _, form := range forms {
		for _, tt := range cases {
			t.Run(form.name+"/"+tt.name, func(t *testing.T) {
				t.Parallel()
				f := form.build(tt.message)
				f.Normalize()
				err := f.Validate()

				if !tt.expectError {
					require.NoError(t, err)
					return
				}

				require.Error(t, err)
				require.ErrorContains(t, err, tt.errorMsg)
				requireHTTPDetail(t, err, agent.ErrEmptyMessage)
			})
		}
	}
}

func TestResolveApprovalForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Approved bool
		Reason   string
		WantErr  bool
	}{
		{
			Name:     "approved without reason",
			Approved: true,
		},
		{
			Name:     "approved with reason",
			Approved: true,
			Reason:   "looks good",
		},
		{
			Name:   "rejected with reason",
			Reason: "not allowed",
		},
		{
			Name:    "rejected without reason",
			WantErr: true,
		},
		{
			Name:    "rejected with whitespace only reason",
			Reason:  "   \t  ",
			WantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			form := agent.ResolveApprovalForm{Approved: tt.Approved, Reason: tt.Reason}
			err := form.Validate()

			if !tt.WantErr {
				require.NoError(t, err)
				return
			}

			require.ErrorContains(t, err, "Invalid approval request")
			requireHTTPDetail(t, err, agent.ErrReasonRequiredForRejection)
		})
	}
}
