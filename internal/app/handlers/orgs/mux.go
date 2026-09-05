package orgs

import (
	"nautilus/internal/database"
	"nautilus/internal/mux"
)

type OrgMux struct {
	db database.Database
}

func NewMux(db database.Database) *OrgMux {
	return &OrgMux{
		db: db,
	}
}

func (o *OrgMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	// Organization invite endpoints
	sub.Post("/:orgId/invites", o.CreateInvite)
	sub.Get("/:orgId/invites", o.ListInvites)
	sub.Delete("/:orgId/invites/:inviteId", o.RevokeInvite)

	// Invite redemption endpoint (doesn't require org context)
	sub.Post("/invites/:token/redeem", o.RedeemInvite)
}
