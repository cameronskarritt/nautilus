package httputil

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"nautilus/internal/errors"
	"nautilus/internal/testutil/require"
)

type requestTarget struct {
	Key string `json:"key"`
}

func TestParseRequestBody(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Body          string
		ContentType   string
		ExpectedKey   string
		ExpectedError *httpErrorAssertion
	}{
		{
			Name:        "valid json",
			Body:        `{"key":"value"}`,
			ContentType: "application/json",
			ExpectedKey: "value",
		},
		{
			Name:        "missing content type",
			Body:        `{"key":"value"}`,
			ExpectedKey: "value",
		},
		{
			Name:        "unsupported content type",
			Body:        `{"key":"value"}`,
			ContentType: "text/plain",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusUnsupportedMediaType,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON07,
				Detail:  "Content-Type must be application/json",
			},
		},
		{
			Name:        "malformed json",
			Body:        `{key: "value"}`,
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Request body is malformed",
				Code:    errors.ErrorCodeJSON02,
				Detail:  "invalid character 'k' looking for beginning of object key string",
			},
		},
		{
			Name:        "invalid field type",
			Body:        `{"key": 123}`,
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Request contains invalid fields",
				Code:    errors.ErrorCodeJSON05,
				Detail:  "key must be string",
				Field:   "key",
			},
		},
		{
			Name:        "body too large",
			Body:        fmt.Sprintf(`{"key": %q}`, strings.Repeat("a", int(maxSize+1))),
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusRequestEntityTooLarge,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON01,
				Detail:  fmt.Sprintf("request body exceeds max size (%d bytes)", maxSize),
			},
		},
		{
			Name:        "empty body",
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON03,
				Detail:  "request body cannot be empty",
			},
		},
		{
			Name:        "unexpected eof",
			Body:        `{"key": "value"`,
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON04,
				Detail:  "unexpected EOF",
			},
		},
		{
			Name:        "multiple json objects",
			Body:        `{"key":"value"}{"key":"value2"}`,
			ContentType: "application/json",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON06,
				Detail:  "request body must be a single object",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := newJSONRequest(tt.Body, tt.ContentType)
			var target requestTarget
			err := ParseRequestBody(req, &target)
			if tt.ExpectedError != nil {
				requireHTTPError(t, err, *tt.ExpectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.ExpectedKey, target.Key)
		})
	}
}

func TestParseBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Body          []byte
		ExpectedKey   string
		ExpectedError *httpErrorAssertion
	}{
		{Name: "valid json", Body: []byte(`{"key":"value"}`), ExpectedKey: "value"},
		{
			Name: "malformed json",
			Body: []byte(`{key: "value"}`),
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Request body is malformed",
				Code:    errors.ErrorCodeJSON02,
				Detail:  "invalid character 'k' looking for beginning of object key string",
			},
		},
		{
			Name: "empty bytes",
			ExpectedError: &httpErrorAssertion{
				Status:  http.StatusBadRequest,
				Message: "Unable to process request",
				Code:    errors.ErrorCodeJSON03,
				Detail:  "request body cannot be empty",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			var target requestTarget
			err := ParseBytes(tt.Body, &target)
			if tt.ExpectedError != nil {
				requireHTTPError(t, err, *tt.ExpectedError)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.ExpectedKey, target.Key)
		})
	}
}

func TestGetIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name       string
		RemoteAddr string
		Expected   string
	}{
		{Name: "with port", RemoteAddr: "192.168.1.1:8080", Expected: "192.168.1.1:8080"},
		{Name: "without port", RemoteAddr: "192.168.1.1", Expected: "192.168.1.1"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			ip, err := GetIP(context.Background(), &http.Request{RemoteAddr: tt.RemoteAddr})
			require.NoError(t, err)
			require.Equal(t, tt.Expected, ip)
		})
	}
}

func TestGetXForwardedFor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Header   string
		Expected string
	}{
		{Name: "single ip", Header: "192.168.1.1", Expected: "192.168.1.1"},
		{Name: "multiple ips", Header: "192.168.1.1, 10.0.0.1, 172.16.0.1", Expected: "172.16.0.1"},
		{Name: "empty header"},
		{Name: "trims spaces", Header: "192.168.1.1, 10.0.0.1", Expected: "10.0.0.1"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := &http.Request{Header: http.Header{}}
			req.Header.Set("X-Forwarded-For", tt.Header)

			ip, err := GetXForwardedFor(context.Background(), req)
			require.NoError(t, err)
			require.Equal(t, tt.Expected, ip)
		})
	}
}

type testForm struct {
	Key   string `json:"key"`
	Value int    `json:"value"`
}

func (f *testForm) Normalize() {
	f.Key = strings.ToUpper(f.Key)
}

func (f *testForm) Validate() error {
	if f.Key == "" {
		return errors.New("key is required")
	}
	if f.Value < 0 {
		return errors.New("value must be non-negative")
	}
	return nil
}

func TestNoopValidator(t *testing.T) {
	t.Parallel()

	require.NoError(t, NoopValidator{}.Validate())
}

func TestProcessForm(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name        string
		Body        string
		Expected    testForm
		ExpectedErr string
	}{
		{
			Name:     "valid form",
			Body:     `{"key":"test","value":42}`,
			Expected: testForm{Key: "TEST", Value: 42},
		},
		{
			Name:        "validation error",
			Body:        `{"key":"test","value":-1}`,
			ExpectedErr: "value must be non-negative",
		},
		{
			Name:        "empty key",
			Body:        `{"key":"","value":10}`,
			ExpectedErr: "key is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			form := &testForm{}
			err := ProcessForm(newJSONRequest(tt.Body, "application/json"), form)
			if tt.ExpectedErr != "" {
				require.Equal(t, tt.ExpectedErr, err.Error())
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Expected, *form)
		})
	}
}

type httpErrorAssertion struct {
	Status  int
	Message string
	Detail  string
	Code    errors.ErrorCode
	Field   string
}

func requireHTTPError(t *testing.T, err error, expected httpErrorAssertion) {
	t.Helper()

	var httpErr *errors.HTTPError
	require.Error(t, err)
	require.True(t, errors.As(err, &httpErr))
	require.Equal(t, expected.Status, httpErr.Status)
	require.Equal(t, expected.Message, httpErr.Message)
	require.Len(t, httpErr.Errors, 1)

	var detail errors.ErrorDetail
	require.True(t, errors.As(httpErr.Errors[0], &detail))
	require.Equal(t, expected.Detail, detail.Message)
	require.Equal(t, expected.Code, detail.Code)
	require.Equal(t, expected.Field, detail.Field)
}

func newJSONRequest(body, contentType string) *http.Request {
	req := &http.Request{
		Body:   io.NopCloser(bytes.NewReader([]byte(body))),
		Header: http.Header{},
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	return req
}
