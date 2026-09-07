package auth

import (
	"context"

	"nautilus/internal/database"
	"nautilus/internal/kms"
	"nautilus/internal/mail"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
)

type Mux struct {
	db      database.Database
	sender  mail.Sender
	counter Counter
	sso     *SSOMux
	keys    kms.KeyManager
}

func NewMux(ctx context.Context, db database.Database, sender mail.Sender, counter Counter, keys kms.KeyManager) *Mux {
	if counter == nil {
		panic("counter must be set")
	}

	return &Mux{
		db:      db,
		sender:  sender,
		counter: counter,
		sso:     NewSSOMux(ctx, db, sender),
		keys:    keys,
	}
}

func (a *Mux) SSOProviders() []string {
	return a.sso.Providers()
}

func (a *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)
	sub.Use(middleware.UserEncryption(a.keys))

	sub.Delete("/sessions", a.Logout)

	if a.sso.Enabled() {
		a.sso.Mount(sub, "/sso")
	}

	sub.Use(middleware.RequireSession(a.db))

	sub.Post("/email/request", a.RequestEmailChange)
	sub.Post("/email/complete", a.CompleteEmailChange)
	sub.Get("/verification/request", a.RequestVerifcation)
	sub.Post("/verification/complete", a.CompleteVerification)
	sub.Post("/organization/switch", a.SwitchOrganization)

	// TOTP setup/disable routes (require authentication)
	sub.Post("/totp/request", a.RequestTOTP)
	sub.Post("/totp/complete", a.CompleteTOTP)
	sub.Post("/totp/disable", a.DisableTOTP)

	// Admin-only routes
	sub.Use(middleware.RequireAdmin)
	sub.Post("/assume", a.Assume)
	sub.Post("/unassume", a.Unassume)
}
