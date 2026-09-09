package webhooks

import (
	"strconv"

	"nautilus/internal/pagination"
)

func pageParams(params pagination.Params, orgID int, scope string) (int, int, error) {
	limit := params.Limit
	if limit <= 0 {
		limit = pagination.DefaultLimit
	}
	limit = min(limit, 100)
	if params.Cursor == nil {
		return limit, 0, nil
	}
	id, ok := params.Cursor["id"].(string)
	organization, orgOK := params.Cursor["organization_id"].(string)
	parent, parentOK := params.Cursor["scope"].(string)
	n, err := strconv.Atoi(id)
	if len(params.Cursor) != 3 || !ok || !orgOK || !parentOK || organization != strconv.Itoa(orgID) || parent != scope || err != nil || n <= 0 || strconv.Itoa(n) != id {
		return 0, 0, ErrInvalidCursor
	}
	return limit, n, nil
}

func pageCursor(id, orgID int, scope string) pagination.Cursor {
	return pagination.Cursor{"id": strconv.Itoa(id), "organization_id": strconv.Itoa(orgID), "scope": scope}
}
