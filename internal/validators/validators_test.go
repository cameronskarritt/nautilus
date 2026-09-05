package validators

import (
	"strings"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

func TestValidateEmail(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Email    string
		Expected bool
	}{
		{Name: "simple address", Email: "user@example.com", Expected: true},
		{Name: "subdomain address", Email: "user@sub.example.com", Expected: true},
		{Name: "plus address", Email: "user+tag@example.com", Expected: true},
		{Name: "empty address", Email: "", Expected: false},
		{Name: "missing at sign", Email: "userexample.com", Expected: false},
		{Name: "quoted local part", Email: `"user name"@example.com`, Expected: false},
		{Name: "domain without dot", Email: "user@example", Expected: false},
		{Name: "domain starts with dot", Email: "user@.example.com", Expected: false},
		{Name: "domain ends with dot", Email: "user@example.com.", Expected: false},
		{Name: "ip domain", Email: "user@127.0.0.1", Expected: false},
		{Name: "bracketed ip domain", Email: "user@[127.0.0.1]", Expected: false},
		{Name: "too long", Email: "user@" + strings.Repeat("a", 316) + ".com", Expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, ValidateEmail(tt.Email))
		})
	}
}

func TestValidateUUID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		UUID     string
		Expected bool
	}{
		{Name: "lowercase uuid", UUID: "123e4567-e89b-12d3-a456-426614174000", Expected: true},
		{Name: "uppercase uuid", UUID: "123E4567-E89B-12D3-A456-426614174000", Expected: true},
		{Name: "empty uuid", UUID: "", Expected: false},
		{Name: "missing hyphens", UUID: "123e4567e89b12d3a456426614174000", Expected: false},
		{Name: "invalid hex", UUID: "123e4567-e89b-12d3-a456-42661417400g", Expected: false},
		{Name: "wrong segment length", UUID: "123e45678-e89b-12d3-a456-426614174000", Expected: false},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, ValidateUUID(tt.UUID))
		})
	}
}

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Password string
		Expected *errors.ErrorDetail
	}{
		{Name: "minimum length", Password: "12345678"},
		{Name: "maximum length", Password: strings.Repeat("a", 256)},
		{
			Name:     "too short",
			Password: "1234567",
			Expected: &errors.ErrorDetail{
				Message: "password must be more than 8 characters",
				Code:    errors.ErrorCodeAUTH06,
				Field:   "password",
			},
		},
		{
			Name:     "too long",
			Password: strings.Repeat("a", 257),
			Expected: &errors.ErrorDetail{
				Message: "password must be less than 256 characters",
				Code:    errors.ErrorCodeAUTH07,
				Field:   "password",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			requireErrorDetail(t, tt.Expected, ValidatePassword(tt.Password))
		})
	}
}

func TestIsAlphanumeric(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Input    string
		Expected bool
	}{
		{Name: "empty string", Input: "", Expected: true},
		{Name: "letters and numbers", Input: "abc123DEF456", Expected: true},
		{Name: "punctuation", Input: "abc123!", Expected: false},
		{Name: "space", Input: "abc 123", Expected: false},
		{Name: "unicode letters", Input: "αβγ", Expected: false},
		{Name: "unicode numbers", Input: "١٢٣", Expected: false},
		{Name: "ascii edge characters", Input: "09AZaz", Expected: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, IsAlphanumeric(tt.Input))
		})
	}
}

func TestValidateUsername(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Username string
		Expected *errors.ErrorDetail
	}{
		{Name: "minimum length", Username: "abc"},
		{Name: "maximum length", Username: "abcdefghij1234567890"},
		{
			Name:     "too short",
			Username: "ab",
			Expected: &errors.ErrorDetail{
				Message: "username must be at least 3 characters",
				Code:    errors.ErrorCodeUSER02,
				Field:   "username",
			},
		},
		{
			Name:     "too long",
			Username: "abcdefghij1234567890x",
			Expected: &errors.ErrorDetail{
				Message: "username must be 20 characters or fewer",
				Code:    errors.ErrorCodeUSER03,
				Field:   "username",
			},
		},
		{
			Name:     "not alphanumeric",
			Username: "alice_example",
			Expected: &errors.ErrorDetail{
				Message: "username must only contain numbers and letters",
				Code:    errors.ErrorCodeUSER04,
				Field:   "username",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			requireErrorDetail(t, tt.Expected, ValidateUsername(tt.Username))
		})
	}
}

func requireErrorDetail(t *testing.T, expected *errors.ErrorDetail, err error) {
	t.Helper()

	if expected == nil {
		require.NoError(t, err)
		return
	}

	var actual errors.ErrorDetail
	require.ErrorAs(t, err, &actual)
	require.Equal(t, *expected, actual)
}
