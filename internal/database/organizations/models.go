package organizations

import (
	"time"

	"nautilus/internal/optional"
)

type Organization struct {
	ID         int    `json:"-"`
	ExternalID string `json:"id"`

	Slug     string                      `json:"slug"`
	Name     string                      `json:"name"`
	Plan     string                      `json:"plan"`
	Personal bool                        `json:"personal"`
	Settings optional.Optional[Settings] `json:"settings,omitzero"`

	CreatedAt time.Time `json:"created_at"`
}

type Settings struct {
	// Add organization-specific settings here as needed
}

type Member struct {
	ID             int                            `json:"-"`
	ExternalID     string                         `json:"id"`
	UserID         int                            `json:"-"`
	OrganizationID int                            `json:"-"`
	Role           Role                           `json:"role"`
	DisplayName    optional.Optional[string]      `json:"display_name,omitzero"`
	Permissions    optional.Optional[Permissions] `json:"permissions,omitzero"`
	CreatedAt      time.Time                      `json:"created_at"`
}

type Permissions struct {
	// Add fine-grained permissions here as needed
	// Example: CanInvite bool `json:"can_invite"`
}

type Invite struct {
	ID         int    `json:"-"`
	ExternalID string `json:"id"`

	OrganizationID int `json:"-"`
	InvitedBy      int `json:"-"`

	Email string `json:"email"`
	Role  Role   `json:"role"`

	ExpiresAt  time.Time                    `json:"expires_at"`
	CreatedAt  time.Time                    `json:"created_at"`
	RedeemedAt optional.Optional[time.Time] `json:"redeemed_at,omitzero"`
}

// IsPending returns true if the invite has not been redeemed and has not expired
func (i *Invite) IsPending() bool {
	return !i.RedeemedAt.Set && time.Now().Before(i.ExpiresAt)
}

// IsExpired returns true if the invite has expired
func (i *Invite) IsExpired() bool {
	return time.Now().After(i.ExpiresAt)
}
