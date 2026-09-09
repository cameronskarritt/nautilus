package database_test

import (
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

func TestOAuthSchema(t *testing.T) {
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
					if path.Dir(name) == "migrations" && path.Base(name) >= "000011" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
				require.NoError(t, err)
				require.Contains(t, applied, 10)
				require.NotContains(t, applied, 11)
				require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			}
			for _, table := range []string{"mcp_oauth_clients", "mcp_oauth_grants", "mcp_oauth_codes", "mcp_oauth_tokens"} {
				var name string
				require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass($1)`, table).Scan(&name))
				require.Equal(t, table, name)
			}
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "mcp_oauth", applied[11].Name)
		})
	}
}
