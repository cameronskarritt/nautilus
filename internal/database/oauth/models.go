package oauth

import (
	"context"

	"nautilus/internal/errors"
)

var (
	ErrInvalidGrant  = errors.New("invalid OAuth grant")
	ErrInvalidClient = errors.New("invalid OAuth client")
	ErrInvalidScope  = errors.New("invalid OAuth scope")
)

type Client struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
}

type Grant struct {
	ID             string `json:"-"`
	ClientID       string `json:"-"`
	UserID         int    `json:"-"`
	OrganizationID int    `json:"-"`
	MemberID       int    `json:"-"`
	Scope          string `json:"-"`
	Resource       string `json:"-"`
}

type GrantOptions struct {
	ClientID, RedirectURI, Resource, Scope, Challenge string
}

type Tokens struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

type contextKey struct{}

func WithContext(ctx context.Context, grant *Grant) context.Context {
	return context.WithValue(ctx, contextKey{}, grant)
}

func FromContext(ctx context.Context) *Grant {
	grant, _ := ctx.Value(contextKey{}).(*Grant)
	return grant
}
