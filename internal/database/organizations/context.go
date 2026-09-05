package organizations

import "context"

type organizationContextKey struct{}

func WithContext(ctx context.Context, org *Organization) context.Context {
	return context.WithValue(ctx, organizationContextKey{}, org)
}

func FromContext(ctx context.Context) *Organization {
	org, ok := ctx.Value(organizationContextKey{}).(*Organization)
	if !ok {
		return nil
	}
	return org
}

type memberContextKey struct{}

func WithMemberContext(ctx context.Context, member *Member) context.Context {
	return context.WithValue(ctx, memberContextKey{}, member)
}

func MemberFromContext(ctx context.Context) *Member {
	member, ok := ctx.Value(memberContextKey{}).(*Member)
	if !ok {
		return nil
	}
	return member
}
