package webhook

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// ValidName validates a name after surrounding whitespace has been trimmed.
func ValidName(name string) bool {
	return name != "" && utf8.ValidString(name) && utf8.RuneCountInString(name) <= 100 && !strings.ContainsFunc(name, unicode.IsControl)
}
