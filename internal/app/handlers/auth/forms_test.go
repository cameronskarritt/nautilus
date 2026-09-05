package auth_test

import (
	"testing"

	"nautilus/internal/app/handlers/auth"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

type formValidator interface {
	Validate() error
}

func TestFormValidationErrorFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name      string
		Form      formValidator
		WantField string
	}{
		{
			Name:      "new password",
			Form:      &auth.ChangePasswordForm{NewPassword: "short"},
			WantField: "new_password",
		},
		{
			Name:      "mfa setup password",
			Form:      &auth.RequestTOTPForm{},
			WantField: "password",
		},
		{
			Name:      "mfa disable password",
			Form:      &auth.DisableTOTPForm{},
			WantField: "password",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			err := tt.Form.Validate()

			var httpErr *errors.HTTPError
			require.ErrorAs(t, err, &httpErr)
			require.Len(t, httpErr.Errors, 1)

			var detail errors.ErrorDetail
			require.ErrorAs(t, httpErr.Errors[0], &detail)
			require.Equal(t, tt.WantField, detail.Field)
		})
	}
}
