package pagination

// Page represents a paginated response with data and pagination metadata.
type Page[T any] struct {
	// Data contains the items for the current page.
	Data []T `json:"data"`
	// HasMore indicates whether there are more items available beyond this page.
	HasMore bool `json:"has_more"`
	// NextCursor is the cursor to use for fetching the next page.
	// Omitted from JSON if there are no more items.
	NextCursor string `json:"next_cursor,omitempty"`
}

// CursorFunc extracts cursor values from an item.
// The returned cursor should contain values for all columns used in ORDER BY clauses.
type CursorFunc[T any] func(T) Cursor

// Build creates a paginated response from query results.
//
// The items slice should contain limit+1 rows to detect whether there are more items.
// If items contains more than limit rows, HasMore will be true and items will be
// trimmed to limit rows in the response.
//
// The cursorFn is called on the last item to generate the next cursor.
func Build[T any](items []T, limit int, cursorFn CursorFunc[T]) Page[T] {
	hasMore := len(items) > limit
	if hasMore {
		items = items[:limit]
	}

	var nextCursor string
	if hasMore && len(items) > 0 {
		nextCursor = Encode(cursorFn(items[len(items)-1]))
	}

	return Page[T]{
		Data:       items,
		HasMore:    hasMore,
		NextCursor: nextCursor,
	}
}
