package httputil

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestClientRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name           string
		Method         string
		Endpoint       string
		Payload        any
		ExpectedStatus int
		ResponseBody   string
		ResponseObject any
		ExpectedError  string
	}{
		{
			Name:           "Successful GET request",
			Method:         http.MethodGet,
			Endpoint:       "/resource",
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":"value"}`,
			ResponseObject: &map[string]string{},
			ExpectedError:  "",
		},
		{
			Name:           "Error in response decoding",
			Method:         http.MethodGet,
			Endpoint:       "/resource",
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":`, // Malformed JSON
			ResponseObject: &map[string]string{},
			ExpectedError:  "error decoding response body: unexpected EOF",
		},
		{
			Name:           "Successful POST request",
			Method:         http.MethodPost,
			Endpoint:       "/resource",
			Payload:        map[string]string{"input": "value"},
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":"value"}`,
			ResponseObject: &map[string]string{},
			ExpectedError:  "",
		},
		{
			Name:           "Error marshaling payload",
			Method:         http.MethodPost,
			Endpoint:       "/resource",
			Payload:        func() {}, // Invalid payload type
			ExpectedStatus: 0,
			ResponseBody:   "",
			ResponseObject: &map[string]string{},
			ExpectedError:  "error marshaling payload: json: unsupported type: func()",
		},
		{
			Name:           "Successful PUT request",
			Method:         http.MethodPut,
			Endpoint:       "/resource",
			Payload:        map[string]string{"input": "updated value"},
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":"updated value"}`,
			ResponseObject: &map[string]string{},
			ExpectedError:  "",
		},
		{
			Name:           "Successful PATCH request",
			Method:         http.MethodPatch,
			Endpoint:       "/resource",
			Payload:        map[string]string{"input": "patched value"},
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":"patched value"}`,
			ResponseObject: &map[string]string{},
			ExpectedError:  "",
		},
		{
			Name:           "Successful DELETE request",
			Method:         http.MethodDelete,
			Endpoint:       "/resource",
			ExpectedStatus: http.StatusOK,
			ResponseBody:   `{"key":"value"}`,
			ResponseObject: &map[string]string{},
			ExpectedError:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			// Create a new test server that will mock the API endpoint
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, tt.Endpoint, r.URL.Path)
				require.Equal(t, tt.Method, r.Method)

				w.WriteHeader(tt.ExpectedStatus)
				w.Write([]byte(tt.ResponseBody))
			}))
			defer server.Close()

			client := NewClient()
			client.client = server.Client()

			var err error
			var resp *http.Response

			switch tt.Method {
			case http.MethodGet:
				resp, err = client.Get(context.Background(), server.URL+tt.Endpoint, tt.ResponseObject)
			case http.MethodPost:
				resp, err = client.Post(context.Background(), server.URL+tt.Endpoint, tt.Payload, tt.ResponseObject)
			case http.MethodPut:
				resp, err = client.Put(context.Background(), server.URL+tt.Endpoint, tt.Payload, tt.ResponseObject)
			case http.MethodPatch:
				resp, err = client.Patch(context.Background(), server.URL+tt.Endpoint, tt.Payload, tt.ResponseObject)
			case http.MethodDelete:
				resp, err = client.Delete(context.Background(), server.URL+tt.Endpoint, tt.ResponseObject)
			}

			if resp != nil {
				defer resp.Body.Close()
			}

			if tt.ExpectedError != "" {
				require.Error(t, err)
				require.Equal(t, tt.ExpectedError, err.Error())
			} else {
				require.NoError(t, err)
				require.NotNil(t, resp)
				require.Equal(t, tt.ExpectedStatus, resp.StatusCode)

				expectedResponse := &map[string]string{}
				if tt.ResponseBody != "" {
					err := json.Unmarshal([]byte(tt.ResponseBody), expectedResponse)
					require.NoError(t, err)
					require.Equal(t, expectedResponse, tt.ResponseObject)
				}
			}
		})
	}
}
