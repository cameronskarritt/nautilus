package apikeys

import "context"

type contextKey struct{}

func WithContext(ctx context.Context, key *Key) context.Context {
	return context.WithValue(ctx, contextKey{}, key)
}

func FromContext(ctx context.Context) *Key {
	key, _ := ctx.Value(contextKey{}).(*Key)
	return key
}
