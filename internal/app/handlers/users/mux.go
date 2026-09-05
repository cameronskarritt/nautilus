package users

import (
	"nautilus/internal/database"
	"nautilus/internal/featureflags"
	"nautilus/internal/mail"
	"nautilus/internal/mux"
)

type UserMux struct {
	db     database.Database
	sender mail.Sender
	flags  featureflags.FeatureFlagger
}

func NewMux(db database.Database, sender mail.Sender, flags featureflags.FeatureFlagger) *UserMux {
	return &UserMux{
		db:     db,
		sender: sender,
		flags:  flags,
	}
}

func (u *UserMux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Get("/", u.GetUserExternal)
	sub.Get("/me", u.Me)
	sub.Patch("/me", u.UpdateUser)
}
