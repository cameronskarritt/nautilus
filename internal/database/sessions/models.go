package sessions

import (
	"time"

	"nautilus/internal/optional"
)

type Session struct {
	ID           int
	Token        string
	UserID       int
	OrgMemberID  optional.Optional[int]
	AssumedBy    optional.Optional[int]
	AssumedOrgID optional.Optional[int]

	Active    bool
	ExpiresAt time.Time
	CreatedAt time.Time

	Metadata optional.Optional[SessionMetadata]
}

type SessionMetadata struct {
	Addr      optional.Optional[string]
	UserAgent optional.Optional[string]
}
