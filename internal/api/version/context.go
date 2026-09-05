package version

import (
	"context"
)

type contextKey struct{}

func FromContext(ctx context.Context) Version {
	vs, ok := ctx.Value(contextKey{}).(Version)
	if !ok {
		return ""
	}
	return vs
}

func WithContext(ctx context.Context, vs Version) context.Context {
	return context.WithValue(ctx, contextKey{}, vs)
}
