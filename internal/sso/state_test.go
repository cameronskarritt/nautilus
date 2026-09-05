package sso

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

const testSecret = "test-secret-key-32-bytes-long!!"

func TestGenerateAndVerifyState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name        string
		Provider    string
		RedirectURL string
	}{
		{
			Name:        "google provider with redirect",
			Provider:    "google",
			RedirectURL: "/dashboard",
		},
		{
			Name:        "microsoft provider without redirect",
			Provider:    "microsoft",
			RedirectURL: "",
		},
		{
			Name:        "github provider with complex redirect",
			Provider:    "github",
			RedirectURL: "/app/settings?tab=profile",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			w := httptest.NewRecorder()
			state, err := GenerateState(w, testSecret, tt.Provider, tt.RedirectURL)
			require.NoError(t, err)
			require.NotEqual(t, "", state)

			cookies := w.Result().Cookies()
			require.Len(t, cookies, 1)
			require.Equal(t, stateCookieName, cookies[0].Name)
			require.Equal(t, state, cookies[0].Value)

			req := httptest.NewRequest(http.MethodGet, "/callback?state="+state, nil)
			req.AddCookie(cookies[0])

			result, err := VerifyState(req, testSecret, state)
			require.NoError(t, err)
			require.Equal(t, tt.Provider, result.Provider)
			require.Equal(t, tt.RedirectURL, result.RedirectURL)
		})
	}
}

func TestVerifyState_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name         string
		SetupFunc    func(*testing.T) (*http.Request, string, string)
		ErrorContain string
	}{
		{
			Name: "missing cookie",
			SetupFunc: func(t *testing.T) (*http.Request, string, string) {
				t.Helper()
				req := httptest.NewRequest(http.MethodGet, "/callback?state=abc", nil)
				return req, testSecret, "abc"
			},
			ErrorContain: "cookie not found",
		},
		{
			Name: "state mismatch",
			SetupFunc: func(t *testing.T) (*http.Request, string, string) {
				t.Helper()
				req, state := validStateRequest(t, testSecret)
				return req, testSecret, state + "tampered"
			},
			ErrorContain: "state mismatch",
		},
		{
			Name: "invalid signature",
			SetupFunc: func(t *testing.T) (*http.Request, string, string) {
				t.Helper()
				req, state := validStateRequest(t, "secret-one-32-bytes-long!!!!!!!!")
				return req, testSecret, state
			},
			ErrorContain: "invalid state signature",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req, secret, state := tt.SetupFunc(t)

			_, err := VerifyState(req, secret, state)
			require.Error(t, err)
			require.Contains(t, err.Error(), tt.ErrorContain)
		})
	}
}

func TestClearState(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	ClearState(w)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)
	require.Equal(t, stateCookieName, cookies[0].Name)
	require.Equal(t, "", cookies[0].Value)
	require.Equal(t, -1, cookies[0].MaxAge)
}

func TestStateExpiration(t *testing.T) {
	t.Parallel()

	w := httptest.NewRecorder()
	_, err := GenerateState(w, testSecret, "google", "")
	require.NoError(t, err)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)

	expectedMaxAge := int(stateTTL.Seconds())
	require.Equal(t, expectedMaxAge, cookies[0].MaxAge)
}

func TestUniqueNonces(t *testing.T) {
	t.Parallel()

	states := make(map[string]struct{})
	for i := 0; i < 32; i++ {
		w := httptest.NewRecorder()
		state, err := GenerateState(w, testSecret, "google", "")
		require.NoError(t, err)

		_, exists := states[state]
		require.False(t, exists, "duplicate state generated")
		states[state] = struct{}{}
	}
}

func validStateRequest(t *testing.T, secret string) (*http.Request, string) {
	t.Helper()

	w := httptest.NewRecorder()
	state, err := GenerateState(w, secret, "google", "")
	require.NoError(t, err)

	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)

	req := httptest.NewRequest(http.MethodGet, "/callback", nil)
	req.AddCookie(cookies[0])
	return req, state
}
