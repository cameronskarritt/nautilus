package auth

import (
	"net/url"
	"strings"
	"unicode"

	"nautilus/internal/config"
)

func ssoRedirect(raw string) *url.URL {
	if raw == "" {
		raw = config.Get[string]("APP_BASE_URL")
	}
	target := parseSSOURL(raw)
	if target == nil {
		return nil
	}
	for _, key := range []string{"APP_BASE_URL", "ADMIN_BASE_URL"} {
		base := parseSSOURL(config.Get[string](key))
		if base != nil && target.Scheme == base.Scheme && strings.EqualFold(target.Host, base.Host) {
			return target
		}
	}
	return nil
}

func parseSSOURL(raw string) *url.URL {
	decoded, err := url.PathUnescape(raw)
	if err != nil || strings.ContainsFunc(decoded, func(r rune) bool {
		return r == '\\' || unicode.IsControl(r)
	}) {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Opaque != "" {
		return nil
	}
	return u
}
