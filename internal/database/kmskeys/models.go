package kmskeys

import (
	"time"

	"nautilus/internal/optional"
)

type Key struct {
	ID             int                    `json:"-"`
	OrganizationID optional.Optional[int] `json:"-"`
	ProviderKeyID  string                 `json:"-"`
	Ciphertext     []byte                 `json:"-"`
	CreatedAt      time.Time              `json:"-"`
}
