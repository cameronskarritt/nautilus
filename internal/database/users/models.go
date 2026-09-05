package users

import (
	"time"

	"nautilus/internal/enums"
	"nautilus/internal/optional"
)

type User struct {
	ID         int    `json:"-"`
	ExternalID string `json:"id"`

	Email    optional.Optional[string] `json:"email,omitzero"`
	Username optional.Optional[string] `json:"username,omitzero"`

	AuthProvider enums.AuthProvider `json:"auth_provider"`
	Verified     bool               `json:"verified"`
	Admin        bool               `json:"admin"`
	MFAEnabled   bool               `json:"mfa_enabled"`
	CreatedAt    time.Time          `json:"created_at"`
}

type UserExternal struct {
	ID         int       `json:"-"`
	ExternalID string    `json:"id"`
	Username   string    `json:"username,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}
