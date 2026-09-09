package httputil

import (
	"net/url"
	"strings"
)

// ParseBaseURL accepts HTTP(S) service URLs without credentials, queries, or
// fragments. It preserves the path and returns nil for invalid URLs.
func ParseBaseURL(raw string) *url.URL {
	u, err := url.Parse(raw)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.ForceQuery || strings.Contains(raw, "#") {
		return nil
	}
	return u
}
