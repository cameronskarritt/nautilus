// Package apikeys stores organization-owned API keys.
package apikeys

import "time"

const (
	ScopeRead  Scope = "read"
	ScopeWrite Scope = "write"

	MaxNameLength = 100
)

type Scope string

func (s Scope) IsValid() bool {
	return s == ScopeRead || s == ScopeWrite
}

type Key struct {
	ID             int       `json:"-"`
	ExternalID     string    `json:"id"`
	OrganizationID int       `json:"-"`
	CreatedBy      int       `json:"-"`
	Name           string    `json:"name"`
	Prefix         string    `json:"prefix"`
	Scopes         []Scope   `json:"scopes"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

type CreateOptions struct {
	Name   string
	Scopes []Scope
}
