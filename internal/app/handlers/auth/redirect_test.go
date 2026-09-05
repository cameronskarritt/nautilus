package auth

import (
	"testing"

	"nautilus/internal/config"
	"nautilus/internal/testutil/require"
)

func setSSOConfig(t *testing.T, adminURL string) {
	t.Helper()
	t.Setenv("APP_BASE_URL", "https://app.example.test")
	t.Setenv("ADMIN_BASE_URL", adminURL)
	t.Setenv("SSO_SIGNING_SECRET", "test-sso-secret")
	config.SetProvider(new(config.EnvProvider))
	t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
}

func TestSSORedirect(t *testing.T) {
	tests := []struct {
		name     string
		redirect string
		want     string
	}{
		{name: "default", want: "https://app.example.test"},
		{name: "user route", redirect: "https://app.example.test/dashboard?tab=profile#details", want: "https://app.example.test/dashboard?tab=profile#details"},
		{name: "admin route", redirect: "http://localhost:5174/dashboard?filter=active", want: "http://localhost:5174/dashboard?filter=active"},
		{name: "case insensitive hostname", redirect: "https://APP.example.test/dashboard", want: "https://APP.example.test/dashboard"},
		{name: "encoded path", redirect: "https://app.example.test/files/a%2Fb?name=some%20file", want: "https://app.example.test/files/a%2Fb?name=some%20file"},
		{name: "untrusted host", redirect: "https://evil.example.test/dashboard"},
		{name: "host suffix", redirect: "https://app.example.test.evil.example.test/dashboard"},
		{name: "different port", redirect: "http://localhost:5173/dashboard"},
		{name: "different scheme", redirect: "http://app.example.test/dashboard"},
		{name: "userinfo", redirect: "https://evil.example.test@app.example.test/dashboard"},
		{name: "username and password", redirect: "https://user:password@app.example.test/dashboard"},
		{name: "relative path", redirect: "/dashboard"},
		{name: "protocol relative", redirect: "//app.example.test/dashboard"},
		{name: "script scheme", redirect: "javascript:alert(1)"},
		{name: "opaque url", redirect: "https:app.example.test/dashboard"},
		{name: "backslash host", redirect: "https://app.example.test\\@evil.example.test"},
		{name: "backslash path", redirect: "https://app.example.test/\\evil.example.test"},
		{name: "encoded backslash", redirect: "https://app.example.test/%5cevil.example.test"},
		{name: "control character", redirect: "https://app.example.test/\n/dashboard"},
		{name: "encoded control", redirect: "https://app.example.test/%0d%0aLocation:evil"},
		{name: "encoded query control", redirect: "https://app.example.test/dashboard?tab=%00"},
		{name: "malformed escape", redirect: "https://app.example.test/%zz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			setSSOConfig(t, "http://localhost:5174")
			got := ssoRedirect(tt.redirect)
			if tt.want == "" {
				require.Nil(t, got)
				return
			}
			require.NotNil(t, got)
			require.Equal(t, tt.want, got.String())
		})
	}
}

func TestSSORedirectWithoutAdminURL(t *testing.T) {
	setSSOConfig(t, "")
	require.Nil(t, ssoRedirect("https://admin.example.test/dashboard"))
	require.NotNil(t, ssoRedirect("https://app.example.test/dashboard"))
}
