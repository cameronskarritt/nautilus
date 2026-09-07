package database_test

import (
	"context"
	"io/fs"
	"os"
	"path"
	"testing"
	"testing/fstest"

	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestComputeChecksum(t *testing.T) {
	t.Parallel()

	tests := []struct {
		Name     string
		Content  []byte
		Expected string
	}{
		{
			Name:     "empty content",
			Content:  []byte{},
			Expected: "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		},
		{
			Name:     "simple content",
			Content:  []byte("hello world"),
			Expected: "b94d27b9934d3e08a52e52d7da7dabfac484efe37a5380ee9088f7ace2efcde9",
		},
	}

	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			t.Parallel()
			result := database.ComputeChecksum(tt.Content)
			require.Equal(t, tt.Expected, result)
		})
	}
}

func TestInitialize(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	rows, err := db.Query(ctx, "SELECT id, name, checksum, ran_at FROM __migrations LIMIT 1")
	require.NoError(t, err)
	rows.Close()

	rows, err = db.Query(ctx, "SELECT id FROM users LIMIT 1")
	require.NoError(t, err)
	rows.Close()
}

func TestMigrate_Success(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)

	_, ok := applied[0]
	require.True(t, ok)
}

func TestMigrate_InitializesFreshDatabase(t *testing.T) {
	t.Parallel()

	db := testutil.SetupEmptyTestDB(t)
	ctx := context.Background()

	err := database.Migrate(ctx, db, postgres.Migrator{})
	require.NoError(t, err)

	rows, err := db.Query(ctx, "SELECT id FROM users LIMIT 1")
	require.NoError(t, err)
	rows.Close()

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)

	_, ok := applied[0]
	require.True(t, ok)

	err = database.Migrate(ctx, db, postgres.Migrator{})
	require.NoError(t, err)
}

func TestMigrateAddsFeatureFlags(t *testing.T) {
	t.Parallel()

	db := setupMigrationBaseline(t)
	ctx := t.Context()
	require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))

	for _, table := range []string{"feature_flags", "feature_flag_associations"} {
		var name string
		err := db.QueryRow(ctx, "SELECT to_regclass($1)", "public."+table).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, table, name)
	}

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)
	_, ok := applied[1]
	require.True(t, ok)
}

func TestMigrateAddsAPIKeys(t *testing.T) {
	t.Parallel()

	db := setupMigrationBaseline(t)
	ctx := t.Context()
	require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))

	var name string
	err := db.QueryRow(ctx, "SELECT to_regclass('public.api_keys')").Scan(&name)
	require.NoError(t, err)
	require.Equal(t, "api_keys", name)

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)
	_, ok := applied[2]
	require.True(t, ok)
}

func TestMigrateAddsAgentFoundation(t *testing.T) {
	t.Parallel()

	db := setupMigrationBaseline(t)
	ctx := t.Context()
	require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))

	for _, table := range []string{
		"outbox_events",
		"agent_streams",
		"agent_events",
		"agent_approvals",
	} {
		var name string
		err := db.QueryRow(ctx, "SELECT to_regclass($1)", "public."+table).Scan(&name)
		require.NoError(t, err)
		require.Equal(t, table, name)
	}

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)
	for id := 3; id <= 6; id++ {
		_, ok := applied[id]
		require.True(t, ok)
	}
}

func TestKMSKeysSchema(t *testing.T) {
	t.Parallel()

	for _, mode := range []string{"snapshot", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var db database.Database
			if mode == "snapshot" {
				db = testutil.SetupEmptyTestDB(t)
				require.NoError(t, database.Initialize(ctx, db, postgres.Migrator{}))
			} else {
				db = setupMigrationBaseline(t)
				old := fstest.MapFS{}
				schema := os.DirFS("schema")
				names, err := fs.Glob(schema, "migrations/*.sql")
				require.NoError(t, err)
				for _, name := range append(names, "_setup.sql") {
					if path.Base(name) >= "000007" && path.Dir(name) == "migrations" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
				require.NoError(t, err)
				require.Contains(t, applied, 6)
				require.NotContains(t, applied, 7)
				require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			}

			var orgID int
			query := "INSERT INTO organizations DEFAULT VALUES RETURNING id"
			if mode == "snapshot" {
				query = "INSERT INTO organizations(slug, name) VALUES ('kms-test', 'KMS test') RETURNING id"
			}
			require.NoError(t, db.QueryRow(ctx, query).Scan(&orgID))
			_, err := db.Exec(ctx, "INSERT INTO kms_keys(organization_id, provider_key_id, ciphertext) VALUES ($1, 'org-key', $2)", orgID, []byte("wrapped org"))
			require.NoError(t, err)
			_, err = db.Exec(ctx, "INSERT INTO kms_keys(provider_key_id, ciphertext) VALUES ('user-key', $1)", []byte("wrapped user"))
			require.NoError(t, err)
			for _, tt := range []struct {
				query string
				args  []any
			}{
				{query: "INSERT INTO kms_keys(organization_id, provider_key_id, ciphertext) VALUES ($1, 'another-org-key', 'wrapped')", args: []any{orgID}},
				{query: "INSERT INTO kms_keys(provider_key_id, ciphertext) VALUES ('another-user-key', 'wrapped')"},
				{query: "INSERT INTO kms_keys(organization_id, provider_key_id, ciphertext) VALUES (999999, 'missing-org-key', 'wrapped')"},
			} {
				_, err = db.Exec(ctx, tt.query, tt.args...)
				require.Error(t, err)
			}
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			var count int
			require.NoError(t, db.QueryRow(ctx, "SELECT count(*) FROM kms_keys").Scan(&count))
			require.Equal(t, 2, count)
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "kms_keys", applied[7].Name)
		})
	}
}

func TestInitialize_RecordsMigrationBaseline(t *testing.T) {
	t.Parallel()

	db := testutil.SetupEmptyTestDB(t)
	ctx := context.Background()

	err := database.Initialize(ctx, db, postgres.Migrator{})
	require.NoError(t, err)

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)

	_, ok := applied[0]
	require.True(t, ok)
}

func TestMigrate_ChecksumMismatch(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, `UPDATE __migrations SET checksum = 'wrong_checksum_value' WHERE id = 0`)
	require.NoError(t, err)

	err = database.Migrate(ctx, db, postgres.Migrator{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "checksum mismatch")
}

func TestMigrate_WorksWithoutLocker(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	err := database.Migrate(ctx, db, postgres.Migrator{})
	require.NoError(t, err)

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)
	require.NotEmpty(t, applied)
}

func TestGetAppliedMigrations_EmptyTable(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	_, err := db.Exec(ctx, "DELETE FROM __migrations")
	require.NoError(t, err)

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)
	require.Len(t, applied, 0)
}

func TestMigrate_Idempotent(t *testing.T) {
	t.Parallel()

	db := testutil.SetupTestDB(t)
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		err := database.Migrate(ctx, db, postgres.Migrator{})
		require.NoError(t, err)
	}

	applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
	require.NoError(t, err)

	_, ok := applied[0]
	require.True(t, ok)
}

func setupMigrationBaseline(t *testing.T) database.Database {
	t.Helper()

	db := testutil.SetupEmptyTestDB(t)
	_, err := db.Exec(t.Context(), `
		CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
		CREATE TABLE __migrations (
			id BIGINT PRIMARY KEY,
			name TEXT NOT NULL,
			checksum TEXT NOT NULL,
			ran_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
		CREATE TABLE users (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY);
		CREATE TABLE organizations (id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY);
	`)
	require.NoError(t, err)

	noop := []byte("-- This is a no-op migration. It is used to test `./cmd/app db migrate`\n")
	_, err = db.Exec(
		t.Context(),
		"INSERT INTO __migrations(id, name, checksum) VALUES ($1, $2, $3)",
		0,
		"noop",
		database.ComputeChecksum(noop),
	)
	require.NoError(t, err)
	return db
}
