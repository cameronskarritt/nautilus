package mail

import (
	"fmt"
	"unicode"

	"nautilus/internal/config"
)

var funcs = map[string]any{
	"SupportEmail": supportEmail,
	"AppURL":       appURL,
	"AuthCodeURL":  authCodeURL,
	"capitalize":   capitalize,
}

func supportEmail() string {
	return "support@example.com"
}

func appURL() string {
	if config.Get("APP_ENV", "development") == "development" {
		return "http://localhost:8081"
	}

	return "https://example.com"
}

func authCodeURL(action string, token string) string {
	return fmt.Sprintf("%s/confirm?action=%s&code=%s", appURL(), action, token)
}

func capitalize(s string) string {
	if len(s) == 0 {
		return s
	}
	runes := []rune(s)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}
