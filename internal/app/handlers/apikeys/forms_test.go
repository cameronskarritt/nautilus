package apikeys

import (
	"strings"
	"testing"

	"nautilus/internal/database/apikeys"
	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestCreateFormNormalizeAndValidate(t *testing.T) {
	t.Parallel()
	form := &CreateForm{
		Name:   " Production ",
		Scopes: []apikeys.Scope{" WRITE ", "read", "write", ""},
	}

	form.Normalize()
	require.NoError(t, form.Validate())
	require.Equal(t, "Production", form.Name)
	require.Equal(t, []apikeys.Scope{apikeys.ScopeWrite, apikeys.ScopeRead}, form.Scopes)
}

func TestCreateFormValidation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		form CreateForm
		code string
	}{
		{name: "empty name", form: validCreateForm(), code: errors.ErrorCodeAPIKEY03},
		{name: "long name", form: validCreateForm(), code: errors.ErrorCodeAPIKEY08},
		{name: "empty scopes", form: validCreateForm(), code: errors.ErrorCodeAPIKEY04},
		{name: "invalid scope", form: validCreateForm(), code: errors.ErrorCodeAPIKEY05},
	}
	tests[0].form.Name = ""
	tests[1].form.Name = strings.Repeat("n", apikeys.MaxNameLength+1)
	tests[2].form.Scopes = nil
	tests[3].form.Scopes = []apikeys.Scope{"admin"}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			err := tt.form.Validate()
			var httpErr *errors.HTTPError
			require.ErrorAs(t, err, &httpErr)
			require.Contains(t, apiKeyErrorCodes(httpErr), tt.code)
		})
	}
}

func validCreateForm() CreateForm {
	return CreateForm{Name: "Production", Scopes: []apikeys.Scope{apikeys.ScopeRead}}
}

func apiKeyErrorCodes(httpErr *errors.HTTPError) string {
	var codes string
	for _, err := range httpErr.Errors {
		if detail, ok := err.(errors.ErrorDetail); ok {
			codes += detail.Code.String()
		}
	}
	return codes
}
