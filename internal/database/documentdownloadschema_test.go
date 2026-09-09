package database_test

import (
	"io/fs"
	"os"
	"path"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentDownloadsSchema(t *testing.T) {
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
					if path.Dir(name) == "migrations" && path.Base(name) >= "000012" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
				require.NoError(t, err)
				require.Contains(t, applied, 11)
				require.NotContains(t, applied, 12)
				require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			}
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "document_downloads", applied[12].Name)
			for _, name := range []string{"document_downloads", "idx_document_downloads_expires_at"} {
				var actual string
				require.NoError(t, db.QueryRow(ctx, `SELECT to_regclass($1)`, name).Scan(&actual))
				require.Equal(t, name, actual)
			}
			var orgID, otherOrgID, userID, docID, otherDocID, keyID, otherKeyID int
			orgQuery := `INSERT INTO organizations DEFAULT VALUES RETURNING id`
			userQuery := `INSERT INTO users DEFAULT VALUES RETURNING id`
			if mode == "snapshot" {
				orgQuery = `INSERT INTO organizations(slug, name) VALUES (uuid_generate_v4()::text, 'Downloads') RETURNING id`
				userQuery = `INSERT INTO users(username) VALUES ('downloads') RETURNING id`
			}
			require.NoError(t, db.QueryRow(ctx, orgQuery).Scan(&orgID))
			require.NoError(t, db.QueryRow(ctx, orgQuery).Scan(&otherOrgID))
			require.NoError(t, db.QueryRow(ctx, userQuery).Scan(&userID))
			for i, org := range []int{orgID, otherOrgID} {
				var doc, key int
				require.NoError(t, db.QueryRow(ctx, `INSERT INTO documents(organization_id, filename, content_type, size, object_key)
     VALUES ($1, 'report.pdf', 'application/pdf', 42, uuid_generate_v4()::text) RETURNING id`, org).Scan(&doc))
				require.NoError(t, db.QueryRow(ctx, `INSERT INTO api_keys(organization_id, created_by, name, token_hash, prefix)
     VALUES ($1, $2, 'Key', $3, 'prefix') RETURNING id`, org, userID, []byte{byte(i)}).Scan(&key))
				if i == 0 {
					docID, keyID = doc, key
				} else {
					otherDocID, otherKeyID = doc, key
				}
			}
			expires := time.Now().Add(5 * time.Minute)
			_, err = db.Exec(ctx, `INSERT INTO document_downloads(token_hash, organization_id, document_id, api_key_id, resource, expires_at)
    VALUES ('first', $1, $2, $3, 'https://example.com/mcp', $4)`, orgID, docID, keyID, expires)
			require.NoError(t, err)
			for _, tt := range []struct {
				name  string
				query string
				args  []any
				code  string
			}{
				{"document organization", `INSERT INTO document_downloads(token_hash, organization_id, document_id, api_key_id, resource, expires_at) VALUES ('other', $1, $2, $3, 'resource', $4)`, []any{orgID, otherDocID, keyID, expires}, "23503"},
				{"key organization", `INSERT INTO document_downloads(token_hash, organization_id, document_id, api_key_id, resource, expires_at) VALUES ('other', $1, $2, $3, 'resource', $4)`, []any{orgID, docID, otherKeyID, expires}, "23503"},
				{"OAuth token reference", `INSERT INTO document_downloads(token_hash, organization_id, document_id, oauth_access_hash, resource, expires_at) VALUES ('other', $1, $2, 'unknown', 'resource', $3)`, []any{orgID, docID, expires}, "23503"},
				{"token uniqueness", `INSERT INTO document_downloads(token_hash, organization_id, document_id, api_key_id, resource, expires_at) VALUES ('first', $1, $2, $3, 'resource', $4)`, []any{orgID, docID, keyID, expires}, "23505"},
			} {
				t.Run(tt.name, func(t *testing.T) {
					err := database.Transact(ctx, db, func(tx database.Database) error {
						_, err := tx.Exec(ctx, tt.query, tt.args...)
						return err
					})
					var pgErr *pgconn.PgError
					require.ErrorAs(t, err, &pgErr)
					require.Equal(t, tt.code, pgErr.Code)
				})
			}
		})
	}
}
