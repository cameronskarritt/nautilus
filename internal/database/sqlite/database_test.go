package sqlite_test

import (
	"context"
	"path/filepath"
	"testing"

	"nautilus/internal/database/sqlite"
	"nautilus/internal/testutil/require"
)

func TestConnect(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "daemon.db")
	db, err := sqlite.Connect(ctx, path)
	require.NoError(t, err)

	_, err = db.Exec(ctx, `CREATE TABLE persisted (id INTEGER PRIMARY KEY);`)
	require.NoError(t, err)
	require.NoError(t, db.Close())

	db, err = sqlite.Connect(ctx, path)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	var table string
	err = db.QueryRow(
		ctx,
		`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'persisted';`,
	).Scan(&table)
	require.NoError(t, err)
	require.Equal(t, "persisted", table)
}
