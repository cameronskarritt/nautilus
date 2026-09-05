package recoverycodes

import (
	"time"

	"nautilus/internal/optional"
)

// RecoveryCode represents a single MFA recovery code stored in the database.
type RecoveryCode struct {
	ID       int
	UserID   int
	CodeHash string
	UsedAt   optional.Optional[time.Time]
}
