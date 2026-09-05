package pagination

import (
	"net/http"
	"strconv"

	"nautilus/internal/errors"
)

const DefaultLimit = 50

// Params holds parsed pagination parameters from an HTTP request.
type Params struct {
	// Limit is the effective limit to use for queries, capped at maxLimit.
	Limit int
	// Cursor is the decoded pagination cursor, or nil if not provided.
	Cursor Cursor
}

// ParseParams extracts pagination parameters from an HTTP request.
// Reads "limit" and "cursor" query parameters.
//
// The maxLimit parameter is the developer-defined maximum allowed limit.
// If limit is not provided or is <= 0, it defaults to maxLimit.
// If limit exceeds maxLimit, it is capped at maxLimit.
//
// Returns an error if the cursor parameter is malformed (invalid base64 or JSON).
func ParseParams(r *http.Request, maxLimit ...int) (Params, error) {
	q := r.URL.Query()

	limit := DefaultLimit
	if len(maxLimit) > 0 {
		limit = maxLimit[0]
	}
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = min(n, limit)
		}
	}

	cursor, err := Decode(q.Get("cursor"))
	if err != nil {
		return Params{}, errors.Wrap(err, "failed to parse cursor")
	}

	return Params{
		Limit:  limit,
		Cursor: cursor,
	}, nil
}

// GetCursor returns the cursor for pagination.
// Implements querybuilder.Paginator.
func (p Params) GetCursor() map[string]any {
	return p.Cursor
}

// GetLimit returns the limit for pagination.
// Implements querybuilder.Paginator.
func (p Params) GetLimit() int {
	return p.Limit
}
