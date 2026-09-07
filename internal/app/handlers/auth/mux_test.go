package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/mux"
	"nautilus/internal/sso"
	"nautilus/internal/testutil/require"
)

func TestMuxRejectsLocalAuthentication(t *testing.T) {
	t.Parallel()

	for _, enabled := range []bool{false, true} {
		name := "without SSO"
		if enabled {
			name = "with SSO"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := &Mux{sso: new(SSOMux)}
			if enabled {
				a.sso.registry = sso.NewRegistry()
				a.sso.registry.Register(new(testSSOProvider))
			}
			router := mux.New()
			a.Mount(router, "/auth")

			for _, tt := range []struct {
				path   string
				status int
			}{
				{path: "/register", status: http.StatusNotFound},
				{path: "/sessions", status: http.StatusMethodNotAllowed},
				{path: "/recovery/request", status: http.StatusNotFound},
				{path: "/recovery/complete", status: http.StatusNotFound},
				{path: "/password", status: http.StatusNotFound},
			} {
				t.Run(tt.path, func(t *testing.T) {
					t.Parallel()
					rec := httptest.NewRecorder()
					router.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/auth"+tt.path, nil))
					require.Equal(t, tt.status, rec.Code)
					require.Empty(t, rec.Result().Cookies())
				})
			}
		})
	}
}

func TestMuxSSOAndLogout(t *testing.T) {
	setSSOConfig(t, "")
	registry := sso.NewRegistry()
	registry.Register(new(testSSOProvider))
	a := &Mux{sso: &SSOMux{registry: registry}}
	router := mux.New()
	a.Mount(router, "/auth")

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/auth/sso/google", nil))
	require.Equal(t, http.StatusFound, rec.Code)
	require.Contains(t, rec.Header().Get("Location"), "https://accounts.example.test/authorize?state=")
	require.Len(t, rec.Result().Cookies(), 1)

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/auth/sessions", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	require.JSONEq(t, `{"message":"Logout successful"}`, rec.Body.String())
}
