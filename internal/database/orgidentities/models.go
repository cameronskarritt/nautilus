package orgidentities

import (
	"time"

	"nautilus/internal/enums"
)

type Identity struct {
	ID         int    `json:"-"`
	ExternalID string `json:"id"`

	OrganizationID int                `json:"-"`
	Provider       enums.AuthProvider `json:"provider"`
	ProviderID     string             `json:"provider_id"`

	CreatedAt time.Time `json:"created_at"`
}
