package mux

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestRouterServeHTTP(t *testing.T) {
	t.Parallel()

	router := New()
	router.Get("/", textHandler("root"))
	router.Get("/home", textHandler("home"))
	router.Get("/users/{userID}/posts/{postID}", func(w http.ResponseWriter, r *http.Request) {
		userID, userOK := PathParam(r, "userID")
		postID, postOK := PathParam(r, "postID")
		if !userOK || !postOK {
			http.Error(w, "missing path params", http.StatusInternalServerError)
			return
		}
		textHandler(userID+":"+postID).ServeHTTP(w, r)
	})
	router.Get("/files/static", textHandler("static"))
	router.Get("/files/{name:[a-zA-Z]+\\.txt}", func(w http.ResponseWriter, r *http.Request) {
		name, ok := PathParam(r, "name")
		if !ok {
			http.Error(w, "missing path param", http.StatusInternalServerError)
			return
		}
		textHandler(name).ServeHTTP(w, r)
	})
	router.Get("/items/{id:<int>}", textHandler("item"))

	tests := []struct {
		Name   string
		Path   string
		Status int
		Body   string
	}{
		{Name: "root", Path: "/", Status: http.StatusOK, Body: "root"},
		{Name: "static route", Path: "/home", Status: http.StatusOK, Body: "home"},
		{Name: "cleaned route", Path: "/home/", Status: http.StatusOK, Body: "home"},
		{Name: "static beats regex", Path: "/files/static", Status: http.StatusOK, Body: "static"},
		{Name: "path params", Path: "/users/123/posts/456", Status: http.StatusOK, Body: "123:456"},
		{Name: "regex param", Path: "/files/readme.txt", Status: http.StatusOK, Body: "readme.txt"},
		{Name: "regex mismatch", Path: "/files/123.txt", Status: http.StatusNotFound},
		{Name: "known int pattern", Path: "/items/42", Status: http.StatusOK, Body: "item"},
		{Name: "known int pattern mismatch", Path: "/items/nope", Status: http.StatusNotFound},
		{Name: "missing route", Path: "/missing", Status: http.StatusNotFound},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			rec := serve(router, http.MethodGet, tt.Path)
			require.Equal(t, tt.Status, rec.Code)
			if tt.Body != "" {
				require.Equal(t, tt.Body, rec.Body.String())
			}
		})
	}
}

func TestRouterMethodHandling(t *testing.T) {
	t.Parallel()

	router := New()
	router.Get("/test", textHandler("get"))
	router.Post("/test", textHandler("post"))

	rec := serve(router, http.MethodPatch, "/test")
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
	require.Equal(t, "GET,POST", rec.Header().Get("Allow"))
	require.Contains(t, rec.Body.String(), "Method Not Allowed")

	rec = serve(router, http.MethodOptions, "/test")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "GET,POST", rec.Header().Get("Allow"))
}

func TestRegexPriority(t *testing.T) {
	t.Parallel()

	router := New()
	router.Get("/items/{id:[0-9]+}", textHandler("first"))
	router.Get("/items/{id:[0-9a-f]+}", textHandler("second"))

	rec := serve(router, http.MethodGet, "/items/123")
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "first", rec.Body.String())
}

func TestRouterMiddleware(t *testing.T) {
	t.Parallel()

	router := New()
	var calls []string
	router.Use(recordCall(&calls, "one"))
	router.Use(recordCall(&calls, "two"))
	router.Get("/test", func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := serve(router, http.MethodGet, "/test")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{"one", "two", "handler"}, calls)
}

func TestRouterSubRouter(t *testing.T) {
	t.Parallel()

	router := New()
	var calls []string
	router.Use(recordCall(&calls, "parent"))

	api := router.SubRouter("/api")
	api.Use(recordCall(&calls, "child"))
	api.SubRouter("/v1").Get("/users", func(w http.ResponseWriter, _ *http.Request) {
		calls = append(calls, "handler")
		w.WriteHeader(http.StatusNoContent)
	})

	rec := serve(router, http.MethodGet, "/api/v1/users")
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, []string{"parent", "child", "handler"}, calls)
}

func TestRouterConfig(t *testing.T) {
	t.Parallel()

	t.Run("custom not found", func(t *testing.T) {
		t.Parallel()

		router := New(Config{
			NotFoundHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("missing"))
			},
		})

		rec := serve(router, http.MethodGet, "/missing")
		require.Equal(t, http.StatusTeapot, rec.Code)
		require.Equal(t, "missing", rec.Body.String())
	})

	t.Run("custom method not allowed", func(t *testing.T) {
		t.Parallel()

		router := New(Config{
			MethodNotAllowedHandler: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusTeapot)
				_, _ = w.Write([]byte("wrong method"))
			},
		})
		router.Get("/test", textHandler("ok"))

		rec := serve(router, http.MethodPost, "/test")
		require.Equal(t, http.StatusTeapot, rec.Code)
		require.Equal(t, "wrong method", rec.Body.String())
	})

	t.Run("middleware wraps default handlers", func(t *testing.T) {
		t.Parallel()

		router := New(Config{
			Middleware: []Middleware{
				func(next http.Handler) http.Handler {
					return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						w.Header().Set("X-Middleware", "called")
						next.ServeHTTP(w, r)
					})
				},
			},
		})

		rec := serve(router, http.MethodGet, "/missing")
		require.Equal(t, http.StatusNotFound, rec.Code)
		require.Equal(t, "called", rec.Header().Get("X-Middleware"))
	})
}

func TestCleanPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Path     string
		Expected string
	}{
		{Name: "empty path", Path: "", Expected: "/"},
		{Name: "root path", Path: "/", Expected: "/"},
		{Name: "double slash", Path: "/foo//bar", Expected: "/foo/bar"},
		{Name: "parent directory", Path: "/foo/bar/../baz", Expected: "/foo/baz"},
		{Name: "relative path", Path: "foo/../bar", Expected: "bar"},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Expected, cleanPath(tt.Path))
		})
	}
}

func TestParsePattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name      string
		Fragment  string
		Param     string
		Regex     string
		ExpectErr bool
	}{
		{Name: "plain segment", Fragment: "users"},
		{Name: "param", Fragment: "{id}", Param: "id"},
		{Name: "custom regex", Fragment: "{id:[0-9]+}", Param: "id", Regex: "^[0-9]+$"},
		{Name: "known int regex", Fragment: "{id:<int>}", Param: "id", Regex: "^\\d+$"},
		{Name: "known string regex", Fragment: "{slug:<string>}", Param: "slug", Regex: "^[^/]+$"},
		{Name: "empty pattern", Fragment: "{}", ExpectErr: true},
		{Name: "missing param name", Fragment: "{:<int>}", ExpectErr: true},
		{Name: "unbalanced braces", Fragment: "{id", ExpectErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			param, re, err := parsePattern(tt.Fragment)
			if tt.ExpectErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tt.Param, param)
			if tt.Regex == "" {
				require.Nil(t, re)
				return
			}
			require.NotNil(t, re)
			require.Equal(t, tt.Regex, re.String())
		})
	}
}

func serve(router *Router, method, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func textHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func recordCall(calls *[]string, name string) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*calls = append(*calls, name)
			next.ServeHTTP(w, r)
		})
	}
}
