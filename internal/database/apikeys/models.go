// Package apikeys stores organization-owned API keys.
package apikeys

import (
	"time"

	"nautilus/internal/enums"
)

const MaxNameLength = 100

type Key struct {
	ID             int           `json:"-"`
	ExternalID     string        `json:"id"`
	OrganizationID int           `json:"-"`
	CreatedBy      int           `json:"-"`
	Name           string        `json:"name"`
	Prefix         string        `json:"prefix"`
	Scopes         []enums.Scope `json:"scopes"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

type CreateOptions struct {
	Name   string
	Scopes []enums.Scope
}
