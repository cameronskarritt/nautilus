package sqlite_test

import (
	"context"
	"testing"
	"testing/fstest"

	"nautilus/internal/database/sqlite"
	"nautilus/internal/testutil/require"
)

var schema = fstest.MapFS{
	"_setup.sql": {
		Data: []byte(`
			CREATE TABLE IF NOT EXISTS __migrations (
				id INTEGER PRIMARY KEY,
				name TEXT NOT NULL,
				checksum TEXT NOT NULL,
				ran_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
			);
		`),
	},
	"host_keys.sql": {
		Data: []byte(`CREATE TABLE IF NOT EXISTS host_keys (id INTEGER PRIMARY KEY);`),
	},
	"migrations/000000_noop.sql": {
		Data: []byte(`-- no-op`),
	},
}

func TestMigrator(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := sqlite.Connect(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	migrator := sqlite.Migrator{}
	require.NoError(t, migrator.Migrate(ctx, db, schema, []string{"host_keys.sql"}))
	require.NoError(t, migrator.Migrate(ctx, db, schema, []string{"host_keys.sql"}))

	var migrations int
	err = db.QueryRow(ctx, `SELECT COUNT(*) FROM __migrations;`).Scan(&migrations)
	require.NoError(t, err)
	require.Equal(t, 1, migrations)

	var table string
	err = db.QueryRow(
		ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'host_keys';`,
	).Scan(&table)
	require.NoError(t, err)
	require.Equal(t, "host_keys", table)
}
