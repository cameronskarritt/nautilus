package version

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"nautilus/internal/testutil/require"
)

type testValidator map[Version]bool

func (v testValidator) Validate(version Version) bool {
	return v[version]
}

func newTestValidator(versions ...Version) testValidator {
	validator := make(testValidator, len(versions))
	for _, version := range versions {
		validator[version] = true
	}
	return validator
}

func versionHandler(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}
}

func serveVersion(t *testing.T, handler http.Handler, version Version) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(WithContext(req.Context(), version))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestVersionMux_ServeHTTP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name          string
		Versions      Versions
		ValidVersions []Version
		Version       Version
		WantStatus    int
		WantBody      string
	}{
		{
			Name: "direct version match",
			Versions: Versions{
				"2025-10-19": versionHandler("v20251019"),
			},
			ValidVersions: []Version{"2025-10-19"},
			Version:       "2025-10-19",
			WantStatus:    http.StatusOK,
			WantBody:      "v20251019",
		},
		{
			Name: "higher version uses latest lower handler",
			Versions: Versions{
				"2025-10-17": versionHandler("v20251017"),
				"2025-10-19": versionHandler("v20251019"),
				"2025-10-18": versionHandler("v20251018"),
			},
			ValidVersions: []Version{"2025-10-17", "2025-10-18", "2025-10-19", "2025-10-20"},
			Version:       "2025-10-20",
			WantStatus:    http.StatusOK,
			WantBody:      "v20251019",
		},
		{
			Name: "invalid version",
			Versions: Versions{
				"2025-10-19": versionHandler("v20251019"),
			},
			ValidVersions: []Version{"2025-10-19"},
			Version:       "invalid-version",
			WantStatus:    http.StatusBadRequest,
		},
		{
			Name: "no matching handler",
			Versions: Versions{
				"2025-10-19": versionHandler("v20251019"),
			},
			ValidVersions: []Version{"2025-10-18", "2025-10-19"},
			Version:       "2025-10-18",
			WantStatus:    http.StatusInternalServerError,
		},
		{
			Name:          "empty versions",
			Versions:      Versions{},
			ValidVersions: []Version{"2025-10-19"},
			Version:       "2025-10-19",
			WantStatus:    http.StatusInternalServerError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			handler := Use(tt.Versions, WithValidator(newTestValidator(tt.ValidVersions...)))
			status, body := serveVersion(t, handler, tt.Version)
			require.Equal(t, tt.WantStatus, status)
			if tt.WantBody != "" {
				require.Equal(t, tt.WantBody, body)
			}
		})
	}
}

func TestVersionMiddleware(t *testing.T) {
	t.Parallel()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(FromContext(r.Context())))
	})

	tests := []struct {
		Name       string
		Header     string
		WantStatus int
		WantBody   string
	}{
		{
			Name:       "valid version",
			Header:     string(Version20260101),
			WantStatus: http.StatusOK,
			WantBody:   string(Version20260101),
		},
		{
			Name:       "missing version defaults to latest",
			WantStatus: http.StatusOK,
			WantBody:   string(VersionLatest),
		},
		{
			Name:       "invalid version",
			Header:     "invalid-version",
			WantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.Header != "" {
				req.Header.Set("X-API-Version", tt.Header)
			}

			rec := httptest.NewRecorder()
			Middleware(handler).ServeHTTP(rec, req)
			require.Equal(t, tt.WantStatus, rec.Code)
			if tt.WantBody != "" {
				require.Equal(t, tt.WantBody, rec.Body.String())
			}
		})
	}
}

func TestVersion_Matches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name  string
		Left  Version
		Right Version
		Want  bool
	}{
		{
			Name:  "same version",
			Left:  Version20260101,
			Right: Version20260101,
			Want:  true,
		},
		{
			Name:  "newer version",
			Left:  "2026-10-20",
			Right: Version20260101,
			Want:  true,
		},
		{
			Name:  "older version",
			Left:  "2025-10-18",
			Right: Version20260101,
		},
		{
			Name:  "empty version",
			Left:  "",
			Right: Version20260101,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, tt.Left.Matches(tt.Right))
		})
	}
}

func TestVersionValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name    string
		Version Version
		Want    bool
	}{
		{
			Name:    "known version",
			Version: Version20260101,
			Want:    true,
		},
		{
			Name:    "unknown version",
			Version: "invalid-version",
		},
		{
			Name:    "empty version",
			Version: "",
		},
	}

	validator := DefaultValidator{}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.Want, tt.Version.Validate())
			require.Equal(t, tt.Want, validator.Validate(tt.Version))
		})
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	require.Equal(t, Version(""), FromContext(context.Background()))
	require.Equal(t, Version20260101, FromContext(WithContext(context.Background(), Version20260101)))
}
