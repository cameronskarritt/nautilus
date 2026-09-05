package httputil

import "strings"

func MatchOrigin(origin string, pattern string) (string, bool) {
	if pattern == "*" || pattern == origin {
		return pattern, true
	}

	// Origin must match scheme and subdomain pattern
	if matchScheme(origin, pattern) && matchSubdomain(origin, pattern) {
		return origin, true
	}
	return "", false
}

func MatchOrigins(origin string, patterns []string) (string, bool) {
	for _, pattern := range patterns {
		origin, match := MatchOrigin(origin, pattern)
		if match {
			return origin, true
		}
	}
	return "", false
}

func matchScheme(origin string, pattern string) bool {
	origin, pattern = strings.ToLower(origin), strings.ToLower(pattern)

	oi := strings.Index(origin, "://")
	pi := strings.Index(pattern, "://")

	return oi != -1 && pi != -1 && origin[:oi] == pattern[:pi]
}

func matchSubdomain(origin string, pattern string) bool {
	origin, pattern = strings.ToLower(origin), strings.ToLower(pattern)

	oi := strings.Index(origin, "://")
	pi := strings.Index(pattern, "://")

	// Trim scheme + ://
	if oi != -1 && pi != -1 {
		origin = origin[oi+3:]
		pattern = pattern[pi+3:]
	}

	// Invalid domain length
	if len(origin) > 253 {
		return false
	}

	// Reverse domain segments for easier comparison
	oSegments := reverseParts(origin)
	pSegments := reverseParts(pattern)

	for i, v := range oSegments {
		// origin has more subdomains than the pattern could match (with no wildcard)
		// "a.b.c.example.com" will not match the pattern "b.c.example.com"
		if len(pSegments) <= i {
			return false
		}

		// Wildcard matches any further subdomains
		// "a.b.c.example.com" will match the pattern "*.example.com"
		if pSegments[i] == "*" {
			return true
		}

		// Origin does not match pattern
		if pSegments[i] != v {
			return false
		}
	}

	// Origin matches if segments are the same length,
	// otherwise the origin does not have enough subdomains to match the pattern
	return len(oSegments) == len(pSegments)
}

// "subdomain.example.com" -> ["com", "example", "subdomain"]
func reverseParts(domain string) []string {
	parts := strings.Split(domain, ".")
	for i := len(parts)/2 - 1; i >= 0; i-- {
		j := len(parts) - i - 1
		parts[i], parts[j] = parts[j], parts[i]
	}
	return parts
}
