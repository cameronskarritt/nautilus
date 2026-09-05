package orgs_test

import (
	"testing"

	"nautilus/internal/app/handlers/orgs"
	"nautilus/internal/database/organizations"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func requireDetails(t *testing.T, err error, details ...errors.ErrorDetail) {
	t.Helper()

	var httpErr *errors.HTTPError
	require.ErrorAs(t, err, &httpErr)
	for _, detail := range details {
		require.Contains(t, httpErr.Errors, detail)
	}
}

func TestCreateInviteForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name       string
		Form       orgs.CreateInviteForm
		WantEmail  string
		WantRole   organizations.Role
		WantErrors []errors.ErrorDetail
	}{
		{
			Name: "valid invite",
			Form: orgs.CreateInviteForm{
				Email: " User@Example.COM ",
				Role:  " admin ",
			},
			WantEmail: "user@example.com",
			WantRole:  organizations.RoleAdmin,
		},
		{
			Name: "missing fields",
			Form: orgs.CreateInviteForm{
				Email: " ",
				Role:  " ",
			},
			WantErrors: []errors.ErrorDetail{orgs.ErrEmptyEmail, orgs.ErrInvalidRole},
		},
		{
			Name: "invalid email",
			Form: orgs.CreateInviteForm{
				Email: "not-email",
				Role:  organizations.RoleMember,
			},
			WantErrors: []errors.ErrorDetail{orgs.ErrInvalidEmail},
		},
		{
			Name: "owner role",
			Form: orgs.CreateInviteForm{
				Email: "user@example.com",
				Role:  organizations.RoleOwner,
			},
			WantErrors: []errors.ErrorDetail{orgs.ErrCannotInviteOwner},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			form := tt.Form
			form.Normalize()
			err := form.Validate()

			if len(tt.WantErrors) == 0 {
				require.NoError(t, err)
				require.Equal(t, tt.WantEmail, form.Email)
				require.Equal(t, tt.WantRole, form.Role)
				return
			}

			require.ErrorContains(t, err, "Unable to process invite")
			requireDetails(t, err, tt.WantErrors...)
		})
	}
}
