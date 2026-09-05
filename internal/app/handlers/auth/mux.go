package auth

import (
	"context"

	"nautilus/internal/database"
	"nautilus/internal/mail"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
)

type Mux struct {
	db      database.Database
	sender  mail.Sender
	counter Counter
	sso     *SSOMux
}

func NewMux(ctx context.Context, db database.Database, sender mail.Sender, counter Counter) *Mux {
	if counter == nil {
		panic("counter must be set")
	}

	return &Mux{
		db:      db,
		sender:  sender,
		counter: counter,
		sso:     NewSSOMux(ctx, db, sender),
	}
}

func (a *Mux) SSOProviders() []string {
	return a.sso.Providers()
}

func (a *Mux) Mount(r *mux.Router, prefix string) {
	sub := r.SubRouter(prefix)

	sub.Post("/register", a.Register)
	sub.Post("/sessions", a.Login)
	sub.Delete("/sessions", a.Logout)
	sub.Post("/recovery/request", a.RequestRecovery)
	sub.Post("/recovery/complete", a.CompleteRecovery)

	if a.sso.Enabled() {
		a.sso.Mount(sub, "/sso")
	}

	sub.Use(middleware.RequireSession(a.db))

	sub.Post("/password", a.ChangePassword)
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
