package httputil

import (
	"strings"
	"testing"

	"nautilus/internal/testutil/require"
)

func TestMatchOrigin(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name           string
		Origin         string
		Pattern        string
		ExpectedMatch  bool
		ExpectedResult string
	}{
		{
			Name:           "Exact match",
			Origin:         "http://example.com",
			Pattern:        "http://example.com",
			ExpectedMatch:  true,
			ExpectedResult: "http://example.com",
		},
		{
			Name:           "Wildcard match",
			Origin:         "http://example.com",
			Pattern:        "*",
			ExpectedMatch:  true,
			ExpectedResult: "*",
		},
		{
			Name:           "Scheme and subdomain match",
			Origin:         "https://sub.example.com",
			Pattern:        "https://*.example.com",
			ExpectedMatch:  true,
			ExpectedResult: "https://sub.example.com",
		},
		{
			Name:           "Non-matching scheme",
			Origin:         "http://example.com",
			Pattern:        "https://example.com",
			ExpectedMatch:  false,
			ExpectedResult: "",
		},
		{
			Name:           "Non-matching subdomain",
			Origin:         "https://other.example.com",
			Pattern:        "https://sub.example.com",
			ExpectedMatch:  false,
			ExpectedResult: "",
		},
		{
			Name:           "Case-insensitive match",
			Origin:         "HTTPS://EXAMPLE.COM",
			Pattern:        "https://example.com",
			ExpectedMatch:  true,
			ExpectedResult: "HTTPS://EXAMPLE.COM",
		},
		{
			Name:           "Complex URL match",
			Origin:         "https://api.service.example.com",
			Pattern:        "https://*.example.com",
			ExpectedMatch:  true,
			ExpectedResult: "https://api.service.example.com",
		},
		{
			Name:           "No match",
			Origin:         "http://example.com",
			Pattern:        "https://other.com",
			ExpectedMatch:  false,
			ExpectedResult: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result, match := MatchOrigin(tt.Origin, tt.Pattern)
			require.Equal(t, tt.ExpectedMatch, match)
			require.Equal(t, tt.ExpectedResult, result)
		})
	}
}

func TestMatchOrigins(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name           string
		Origin         string
		Patterns       []string
		ExpectedMatch  bool
		ExpectedResult string
	}{
		{
			Name:           "Single exact match",
			Origin:         "http://example.com",
			Patterns:       []string{"http://example.com"},
			ExpectedMatch:  true,
			ExpectedResult: "http://example.com",
		},
		{
			Name:           "Single wildcard match",
			Origin:         "http://example.com",
			Patterns:       []string{"*"},
			ExpectedMatch:  true,
			ExpectedResult: "*",
		},
		{
			Name:           "Multiple patterns with match",
			Origin:         "https://sub.example.com",
			Patterns:       []string{"http://other.com", "https://*.example.com", "http://another.com"},
			ExpectedMatch:  true,
			ExpectedResult: "https://sub.example.com",
		},
		{
			Name:           "No matching patterns",
			Origin:         "http://example.com",
			Patterns:       []string{"https://example.com", "http://other.com"},
			ExpectedMatch:  false,
			ExpectedResult: "",
		},
		{
			Name:           "Empty patterns list",
			Origin:         "http://example.com",
			Patterns:       []string{},
			ExpectedMatch:  false,
			ExpectedResult: "",
		},
		{
			Name:           "Case-insensitive match in patterns",
			Origin:         "HTTPS://EXAMPLE.COM",
			Patterns:       []string{"http://other.com", "https://example.com"},
			ExpectedMatch:  true,
			ExpectedResult: "HTTPS://EXAMPLE.COM",
		},
		{
			Name:           "Complex URL match in patterns",
			Origin:         "https://api.service.example.com",
			Patterns:       []string{"http://other.com", "https://*.example.com", "http://example.com"},
			ExpectedMatch:  true,
			ExpectedResult: "https://api.service.example.com",
		},
		{
			Name:           "Wildcard prioritized over exact match",
			Origin:         "http://example.com",
			Patterns:       []string{"*", "http://example.com", "https://example.com"},
			ExpectedMatch:  true,
			ExpectedResult: "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result, match := MatchOrigins(tt.Origin, tt.Patterns)
			require.Equal(t, tt.ExpectedMatch, match)
			require.Equal(t, tt.ExpectedResult, result)
		})
	}
}

func TestMatchScheme(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Origin   string
		Pattern  string
		Expected bool
	}{
		{
			Name:     "Matching schemes with http",
			Origin:   "http://example.com",
			Pattern:  "http://example.org",
			Expected: true,
		},
		{
			Name:     "Matching schemes with https",
			Origin:   "https://example.com",
			Pattern:  "https://example.org",
			Expected: true,
		},
		{
			Name:     "Non-matching schemes http and https",
			Origin:   "http://example.com",
			Pattern:  "https://example.org",
			Expected: false,
		},
		{
			Name:     "Non-matching schemes with ftp and http",
			Origin:   "ftp://example.com",
			Pattern:  "http://example.org",
			Expected: false,
		},
		{
			Name:     "Origin missing scheme",
			Origin:   "example.com",
			Pattern:  "http://example.org",
			Expected: false,
		},
		{
			Name:     "Pattern missing scheme",
			Origin:   "http://example.com",
			Pattern:  "example.org",
			Expected: false,
		},
		{
			Name:     "Both missing schemes",
			Origin:   "example.com",
			Pattern:  "example.org",
			Expected: false,
		},
		{
			Name:     "Case insensitive matching schemes",
			Origin:   "HTTP://example.com",
			Pattern:  "http://example.org",
			Expected: true,
		},
		{
			Name:     "Case insensitive non-matching schemes",
			Origin:   "HTTP://example.com",
			Pattern:  "HTTPS://example.org",
			Expected: false,
		},
		{
			Name:     "Complex URLs with matching schemes",
			Origin:   "https://user:pass@example.com:8080/path?query#fragment",
			Pattern:  "https://example.org/path",
			Expected: true,
		},
		{
			Name:     "Complex URLs with non-matching schemes",
			Origin:   "http://user:pass@example.com:8080/path?query#fragment",
			Pattern:  "https://example.org/path",
			Expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := matchScheme(tt.Origin, tt.Pattern)
			require.Equal(t, tt.Expected, result)
		})
	}
}

func TestMatchSubdomain(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Origin   string
		Pattern  string
		Expected bool
	}{
		{
			Name:     "Exact match without subdomains",
			Origin:   "http://example.com",
			Pattern:  "http://example.com",
			Expected: true,
		},
		{
			Name:     "Exact match with subdomains",
			Origin:   "http://sub.example.com",
			Pattern:  "http://sub.example.com",
			Expected: true,
		},
		{
			Name:     "Subdomain match with wildcard",
			Origin:   "http://a.b.example.com",
			Pattern:  "http://*.example.com",
			Expected: true,
		},
		{
			Name:     "Non-matching subdomain",
			Origin:   "http://sub.example.com",
			Pattern:  "http://other.example.com",
			Expected: false,
		},
		{
			Name:     "Non-matching subdomain with wildcard",
			Origin:   "http://sub.example.com",
			Pattern:  "http://*.other.com",
			Expected: false,
		},
		{
			Name:     "More subdomains than pattern",
			Origin:   "http://a.b.c.example.com",
			Pattern:  "http://b.c.example.com",
			Expected: false,
		},
		{
			Name:     "Matching multiple subdomains with wildcard",
			Origin:   "http://a.b.c.example.com",
			Pattern:  "http://*.c.example.com",
			Expected: true,
		},
		{
			Name:     "Case insensitive match",
			Origin:   "http://Sub.Example.Com",
			Pattern:  "http://sub.example.com",
			Expected: true,
		},
		{
			Name:     "Case insensitive wildcard match",
			Origin:   "http://A.B.Example.Com",
			Pattern:  "http://*.example.com",
			Expected: true,
		},
		{
			Name:     "Origin with invalid domain length",
			Origin:   "http://" + strings.Repeat("a", 254) + ".com",
			Pattern:  "http://example.com",
			Expected: false,
		},
		{
			Name:     "Matching complex subdomain pattern",
			Origin:   "http://api.v2.service.example.com",
			Pattern:  "http://*.service.example.com",
			Expected: true,
		},
		{
			Name:     "Non-matching complex subdomain pattern",
			Origin:   "http://v2.service.example.com",
			Pattern:  "http://*.v2.service.example.com",
			Expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := matchSubdomain(tt.Origin, tt.Pattern)
			require.Equal(t, tt.Expected, result)
		})
	}
}

func TestReverseParts(t *testing.T) {
	t.Parallel()
	tests := []struct {
		Name     string
		Domain   string
		Expected []string
	}{
		{
			Name:     "Simple domain",
			Domain:   "example.com",
			Expected: []string{"com", "example"},
		},
		{
			Name:     "Subdomain domain",
			Domain:   "subdomain.example.com",
			Expected: []string{"com", "example", "subdomain"},
		},
		{
			Name:     "Multiple subdomains",
			Domain:   "a.b.c.example.com",
			Expected: []string{"com", "example", "c", "b", "a"},
		},
		{
			Name:     "Single part domain",
			Domain:   "localhost",
			Expected: []string{"localhost"},
		},
		{
			Name:     "Empty domain",
			Domain:   "",
			Expected: []string{""},
		},
		{
			Name:     "Domain with trailing dot",
			Domain:   "example.com.",
			Expected: []string{"", "com", "example"},
		},
		{
			Name:     "Domain with leading dot",
			Domain:   ".example.com",
			Expected: []string{"com", "example", ""},
		},
		{
			Name:     "Domain with consecutive dots",
			Domain:   "example..com",
			Expected: []string{"com", "", "example"},
		},
		{
			Name:     "IP address",
			Domain:   "192.168.0.1",
			Expected: []string{"1", "0", "168", "192"},
		},
		{
			Name:     "Domain with hyphens",
			Domain:   "sub-domain.example.com",
			Expected: []string{"com", "example", "sub-domain"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := reverseParts(tt.Domain)
			require.Equal(t, tt.Expected, result)
		})
	}
}
