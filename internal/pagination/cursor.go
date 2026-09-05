package pagination

import (
	"encoding/base64"
	"encoding/json"

	"nautilus/internal/errors"
)

// Cursor represents pagination cursor values as a map of column names to values.
// The keys should match the columns used in ORDER BY clauses.
type Cursor map[string]any

// Encode converts a Cursor to an opaque base64-encoded string.
// Returns an empty string if the cursor is nil or empty.
func Encode(c Cursor) string {
	if len(c) == 0 {
		return ""
	}

	b, err := json.Marshal(c)
	if err != nil {
		return ""
	}

	return base64.RawURLEncoding.EncodeToString(b)
}

// Decode parses a base64-encoded cursor string into a Cursor.
// Returns nil cursor and no error if the input string is empty.
// Returns an error if the cursor is malformed (invalid base64 or invalid JSON).
func Decode(s string) (Cursor, error) {
	if s == "" {
		return nil, nil
	}

	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return nil, errors.Wrap(err, "invalid cursor encoding")
	}

	var c Cursor
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, errors.Wrap(err, "invalid cursor format")
	}

	return c, nil
}
