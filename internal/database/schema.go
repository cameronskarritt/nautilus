package database

import (
	"context"
	"embed"
	"io/fs"

	"nautilus/internal/errors"
)

//go:embed schema/*
var schemaFS embed.FS

var schemaFiles = []string{
	"users.sql",
	"organizations.sql",
	"api_keys.sql",
	"organization_identities.sql",
	"org_members.sql",
	"sessions.sql",
	"auth_codes.sql",
	"org_invites.sql",
	"mfa_recovery_codes.sql",
	"feature_flags.sql",
	"outbox_events.sql",
	"agent_streams.sql",
	"agent_events.sql",
	"agent_approvals.sql",
	"audit_logs.sql",
	"push_subscriptions.sql",
	"kms_keys.sql",
}

func Initialize(ctx context.Context, db Database, migrator Migrator) error {
	schema, err := fs.Sub(schemaFS, "schema")
	if err != nil {
		return errors.Wrap(err, "unable to load schema")
	}
	return migrator.Initialize(ctx, db, schema, schemaFiles)
}

func Migrate(ctx context.Context, db Database, migrator Migrator) error {
	schema, err := fs.Sub(schemaFS, "schema")
	if err != nil {
		return errors.Wrap(err, "unable to load schema")
	}
	return migrator.Migrate(ctx, db, schema, schemaFiles)
}
