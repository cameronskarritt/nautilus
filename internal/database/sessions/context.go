package sessions

import (
	"context"

	"nautilus/internal/optional"
)

type contextKey struct{}
type assumedOrgKey struct{}

func WithContext(ctx context.Context, sessionID int) context.Context {
	return context.WithValue(ctx, contextKey{}, sessionID)
}

func FromContext(ctx context.Context) int {
	sessionID, ok := ctx.Value(contextKey{}).(int)
	if !ok {
		return -1
	}

	return sessionID
}

// WithAssumedOrgID stores the assumed org ID in context (for admin org override)
func WithAssumedOrgID(ctx context.Context, orgID optional.Optional[int]) context.Context {
	return context.WithValue(ctx, assumedOrgKey{}, orgID)
}

// AssumedOrgIDFromContext retrieves the assumed org ID from context
func AssumedOrgIDFromContext(ctx context.Context) optional.Optional[int] {
	orgID, ok := ctx.Value(assumedOrgKey{}).(optional.Optional[int])
	if !ok {
		return optional.Empty[int]()
	}
	return orgID
}
