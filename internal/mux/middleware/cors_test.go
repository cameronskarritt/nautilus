package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestCORS_SimpleRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name        string
		Config      *CORSConfig
		Origin      string
		AllowOrigin string
		Vary        string
		Credentials string
		Expose      string
	}{
		{Name: "default wildcard", Origin: "https://example.com", AllowOrigin: "*"},
		{
			Name:        "matching origin",
			Config:      &CORSConfig{AllowOrigins: "https://example.com"},
			Origin:      "https://example.com",
			AllowOrigin: "https://example.com",
			Vary:        "Origin",
		},
		{
			Name:   "non-matching origin",
			Config: &CORSConfig{AllowOrigins: "https://example.com"},
			Origin: "https://other.com",
			Vary:   "Origin",
		},
		{
			Name:        "wildcard with credentials",
			Config:      &CORSConfig{AllowOrigins: "*", AllowCredentials: true},
			Origin:      "https://example.com",
			AllowOrigin: "https://example.com",
			Credentials: "true",
		},
		{
			Name:        "exposed headers",
			Config:      &CORSConfig{AllowOrigins: "*", ExposeHeaders: "X-Custom-Header"},
			Origin:      "https://example.com",
			AllowOrigin: "*",
			Expose:      "X-Custom-Header",
		},
		{
			Name:        "second listed origin",
			Config:      &CORSConfig{AllowOrigins: "https://example.com, https://other.com"},
			Origin:      "https://other.com",
			AllowOrigin: "https://other.com",
			Vary:        "Origin",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			rec, called := serveCORS(http.MethodGet, tt.Origin, tt.Config, nil)
			require.True(t, called)
			require.Equal(t, http.StatusOK, rec.Code)
			require.Equal(t, tt.AllowOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, tt.Vary, rec.Header().Get("Vary"))
			require.Equal(t, tt.Credentials, rec.Header().Get("Access-Control-Allow-Credentials"))
			require.Equal(t, tt.Expose, rec.Header().Get("Access-Control-Expose-Headers"))
		})
	}
}

func TestCORS_PreflightRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name           string
		Config         *CORSConfig
		Origin         string
		RequestHeaders string
		AllowOrigin    string
		AllowMethods   string
		AllowHeaders   string
		Credentials    string
		MaxAge         string
		Vary           string
	}{
		{
			Name:        "default wildcard",
			Origin:      "https://example.com",
			AllowOrigin: "*",
			Vary:        "Access-Control-Request-Headers, Access-Control-Request-Method",
		},
		{
			Name:        "specific origin",
			Config:      &CORSConfig{AllowOrigins: "https://example.com"},
			Origin:      "https://example.com",
			AllowOrigin: "https://example.com",
			Vary:        "Access-Control-Request-Headers, Access-Control-Request-Method, Origin",
		},
		{
			Name:         "configured methods headers and max age",
			Config:       &CORSConfig{AllowOrigins: "*", AllowMethods: []string{"GET", "POST"}, AllowHeaders: "Content-Type", MaxAge: 3600},
			Origin:       "https://example.com",
			AllowOrigin:  "*",
			AllowMethods: "GET,POST",
			AllowHeaders: "Content-Type",
			MaxAge:       "3600",
			Vary:         "Access-Control-Request-Headers, Access-Control-Request-Method",
		},
		{
			Name:           "falls back to request headers",
			Config:         &CORSConfig{AllowOrigins: "*"},
			Origin:         "https://example.com",
			RequestHeaders: "X-Custom-Header",
			AllowOrigin:    "*",
			AllowHeaders:   "X-Custom-Header",
			Vary:           "Access-Control-Request-Headers, Access-Control-Request-Method",
		},
		{
			Name:        "credentials",
			Config:      &CORSConfig{AllowOrigins: "*", AllowCredentials: true},
			Origin:      "https://example.com",
			AllowOrigin: "https://example.com",
			Credentials: "true",
			Vary:        "Access-Control-Request-Headers, Access-Control-Request-Method",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			rec, called := serveCORS(http.MethodOptions, tt.Origin, tt.Config, map[string]string{
				"Access-Control-Request-Headers": tt.RequestHeaders,
			})
			require.False(t, called)
			require.Equal(t, http.StatusNoContent, rec.Code)
			require.Equal(t, tt.AllowOrigin, rec.Header().Get("Access-Control-Allow-Origin"))
			require.Equal(t, tt.AllowMethods, rec.Header().Get("Access-Control-Allow-Methods"))
			require.Equal(t, tt.AllowHeaders, rec.Header().Get("Access-Control-Allow-Headers"))
			require.Equal(t, tt.Credentials, rec.Header().Get("Access-Control-Allow-Credentials"))
			require.Equal(t, tt.MaxAge, rec.Header().Get("Access-Control-Max-Age"))
			require.Equal(t, tt.Vary, rec.Header().Get("Vary"))
		})
	}
}

func TestCORSConfig_Origins(t *testing.T) {
	t.Parallel()

	config := &CORSConfig{AllowOrigins: "https://example.com, https://other.com"}
	require.Equal(t, []string{"https://example.com", "https://other.com"}, config.Origins())
}

func serveCORS(method, origin string, config *CORSConfig, headers map[string]string) (*httptest.ResponseRecorder, bool) {
	called := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(method, "/", nil)
	req.Header.Set("Origin", origin)
	for key, value := range headers {
		if value != "" {
			req.Header.Set(key, value)
		}
	}
	rec := httptest.NewRecorder()
	CORS(config)(handler).ServeHTTP(rec, req)
	return rec, called
}
