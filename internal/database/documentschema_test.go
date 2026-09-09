package database_test

import (
	"io/fs"
	"os"
	"path"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nautilus/internal/database"
	"nautilus/internal/database/postgres"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentsSchemaDefaults(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "document-defaults", "Documents")
	var id, size int
	var externalID, filename, contentType, objectKey, status, sha256 string
	var createdAt, updatedAt time.Time
	err := db.QueryRow(ctx, `
		INSERT INTO documents(organization_id, filename, content_type, size, object_key)
		VALUES ($1, 'letter.pdf', 'application/pdf', 42, 'documents/test-object')
		RETURNING id, external_id, filename, content_type, size, object_key, status, created_at, updated_at, sha256;
	`, orgID).Scan(&id, &externalID, &filename, &contentType, &size, &objectKey, &status, &createdAt, &updatedAt, &sha256)
	require.NoError(t, err)
	require.Positive(t, id)
	require.NotEmpty(t, externalID)
	require.Equal(t, "letter.pdf", filename)
	require.Equal(t, "application/pdf", contentType)
	require.Equal(t, 42, size)
	require.Equal(t, "documents/test-object", objectKey)
	require.Equal(t, enums.DocumentStatusUploading.String(), status)
	require.False(t, createdAt.IsZero())
	require.Equal(t, createdAt, updatedAt)
	require.Empty(t, sha256)
}

func TestDocumentsSchemaConstraints(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		code  string
	}{
		{
			name:  "hash is not nullable",
			query: `UPDATE documents SET sha256 = NULL`,
			code:  "23502",
		},
		{
			name: "organization foreign key",
			query: `INSERT INTO documents(organization_id, filename, content_type, size, object_key)
				VALUES (-1, 'letter.pdf', 'application/pdf', 42, 'other-object')`,
			code: "23503",
		},
		{
			name: "external ID uniqueness",
			query: `INSERT INTO documents(organization_id, external_id, filename, content_type, size, object_key)
				SELECT organization_id, external_id, filename, content_type, size, 'other-object' FROM documents`,
			code: "23505",
		},
		{
			name: "object key uniqueness",
			query: `INSERT INTO documents(organization_id, filename, content_type, size, object_key)
				SELECT organization_id, filename, content_type, size, object_key FROM documents`,
			code: "23505",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx := t.Context()
			orgID := testutil.CreateTestOrg(t, db, "document-constraints", "Documents")
			_, err := db.Exec(ctx, `
				INSERT INTO documents(organization_id, filename, content_type, size, object_key)
				VALUES ($1, 'letter.pdf', 'application/pdf', 42, 'documents/test-object');
			`, orgID)
			require.NoError(t, err)
			_, err = db.Exec(ctx, tt.query)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, tt.code, pgErr.Code)
		})
	}
}

func TestDocumentsSchemaStorageBoundary(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	rows, err := db.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'documents'
		ORDER BY ordinal_position;
	`)
	require.NoError(t, err)
	var columns []string
	require.NoError(t, database.ScanRows(rows, func(row database.Row) error {
		var name string
		if err := row.Scan(&name); err != nil {
			return err
		}
		columns = append(columns, name)
		return nil
	}))
	// Document bodies and extracted text belong outside the metadata table.
	require.Equal(t, []string{
		"id", "external_id", "organization_id", "filename", "content_type",
		"size", "object_key", "status", "created_at", "updated_at", "page_count", "pdf_key", "sha256", "upload_token", "upload_expires_at", "upload_ready",
	}, columns)

	var tenantKey bool
	require.NoError(t, db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM pg_constraint WHERE conrelid = 'documents'::regclass
			AND contype = 'u' AND pg_get_constraintdef(oid) = 'UNIQUE (organization_id, id)'
		);
	`).Scan(&tenantKey))
	require.True(t, tenantKey)

	var index string
	require.NoError(t, db.QueryRow(ctx, `
		SELECT indexdef FROM pg_indexes
		WHERE schemaname = 'public' AND tablename = 'documents'
		AND indexname = 'idx_documents_organization_id';
	`).Scan(&index))
	require.Contains(t, index, "(organization_id, id DESC)")
}

func TestDocumentStatusSchema(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"snapshot", "upgrade", "upgrade without documents"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var db database.Database
			var orgID int
			legacy := map[string]enums.DocumentStatus{
				"ready": enums.DocumentStatusUploaded, "pending": enums.DocumentStatusFailed,
				"uploading": enums.DocumentStatusUploading, "uploaded": enums.DocumentStatusUploaded,
				"failed": enums.DocumentStatusFailed,
			}
			if mode == "snapshot" {
				db = testutil.SetupEmptyTestDB(t)
				require.NoError(t, database.Initialize(ctx, db, postgres.Migrator{}))
				orgID = testutil.CreateTestOrg(t, db, "status-schema", "Documents")
			} else {
				db = setupMigrationBaseline(t)
				old := fstest.MapFS{}
				schema := os.DirFS("schema")
				names, err := fs.Glob(schema, "migrations/*.sql")
				require.NoError(t, err)
				for _, name := range append(names, "_setup.sql") {
					if path.Base(name) >= "000008" && path.Dir(name) == "migrations" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
				require.NoError(t, err)
				require.Contains(t, applied, 7)
				require.NotContains(t, applied, 8)
				require.NoError(t, db.QueryRow(ctx, "INSERT INTO organizations DEFAULT VALUES RETURNING id").Scan(&orgID))
				if mode == "upgrade" {
					data, err := fs.ReadFile(schema, "documents.sql")
					require.NoError(t, err)
					previous := strings.NewReplacer(
						"DEFAULT 'uploading'", "DEFAULT 'pending'",
						"idx_documents_organization_id", "idx_documents_organization_status_id",
						"(organization_id, id DESC)", "(organization_id, status, id DESC)",
					).Replace(string(data))
					_, err = db.Exec(ctx, previous)
					require.NoError(t, err)
					for status := range legacy {
						_, err = db.Exec(ctx, `INSERT INTO documents(organization_id, filename, content_type, size, object_key, status, updated_at)
							VALUES ($1, 'letter.pdf', 'application/pdf', 42, $2, $2, '2000-01-01T00:00:00Z')`, orgID, status)
						require.NoError(t, err)
					}
				}
				require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			}
			var status enums.DocumentStatus
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO documents(organization_id, filename, content_type, size, object_key)
				VALUES ($1, 'new.pdf', 'application/pdf', 42, 'new-document') RETURNING status`, orgID).Scan(&status))
			require.Equal(t, enums.DocumentStatusUploading, status)
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			if mode == "upgrade" {
				for key, want := range legacy {
					var updatedAt time.Time
					require.NoError(t, db.QueryRow(ctx, "SELECT status, updated_at FROM documents WHERE object_key = $1", key).Scan(&status, &updatedAt))
					require.Equal(t, want, status)
					require.True(t, updatedAt.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)))
				}
			}
			var index string
			require.NoError(t, db.QueryRow(ctx, "SELECT indexdef FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_documents_organization_id'").Scan(&index))
			require.Contains(t, index, "(organization_id, id DESC)")
			var oldIndex bool
			require.NoError(t, db.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM pg_indexes WHERE schemaname = 'public' AND indexname = 'idx_documents_organization_status_id')").Scan(&oldIndex))
			require.False(t, oldIndex)
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "document_status", applied[8].Name)
		})
	}
}

func TestDocumentPagesSchema(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"snapshot", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var db database.Database
			var orgID, otherID, docID int
			if mode == "snapshot" {
				db = testutil.SetupEmptyTestDB(t)
				require.NoError(t, database.Initialize(ctx, db, postgres.Migrator{}))
				orgID = testutil.CreateTestOrg(t, db, "pages-schema", "Pages")
				otherID = testutil.CreateTestOrg(t, db, "other-pages", "Other")
			} else {
				db = setupMigrationBaseline(t)
				old := fstest.MapFS{}
				schema := os.DirFS("schema")
				names, err := fs.Glob(schema, "migrations/*.sql")
				require.NoError(t, err)
				for _, name := range append(names, "_setup.sql") {
					if path.Base(name) >= "000009" && path.Dir(name) == "migrations" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				require.NoError(t, db.QueryRow(ctx, "INSERT INTO organizations DEFAULT VALUES RETURNING id").Scan(&orgID))
				require.NoError(t, db.QueryRow(ctx, "INSERT INTO organizations DEFAULT VALUES RETURNING id").Scan(&otherID))
			}
			require.NoError(t, db.QueryRow(ctx, `INSERT INTO documents(organization_id, filename, content_type, size, object_key)
    VALUES ($1, 'legacy.pdf', 'application/pdf', 42, 'legacy') RETURNING id`, orgID).Scan(&docID))
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			var count int
			var pdfKey string
			require.NoError(t, db.QueryRow(ctx, "SELECT page_count, pdf_key FROM documents WHERE id = $1", docID).Scan(&count, &pdfKey))
			require.Zero(t, count)
			require.Empty(t, pdfKey)
			_, err := db.Exec(ctx, `INSERT INTO document_pages(organization_id, document_id, number, content_type, size, object_key)
    VALUES ($1,$2,1,'image/png',1,'legacy/pages/1')`, orgID, docID)
			require.NoError(t, err)
			_, err = db.Exec(ctx, `INSERT INTO document_pages(organization_id, document_id, number, content_type, size, object_key)
    VALUES ($1,$2,2,'image/png',1,'invalid')`, otherID, docID)
			var pgErr *pgconn.PgError
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23503", pgErr.Code)
			_, err = db.Exec(ctx, `INSERT INTO document_pages(organization_id, document_id, number, content_type, size, object_key)
    VALUES ($1,$2,1,'image/png',1,'duplicate')`, orgID, docID)
			require.ErrorAs(t, err, &pgErr)
			require.Equal(t, "23505", pgErr.Code)
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			require.NoError(t, db.QueryRow(ctx, "SELECT count(*) FROM document_pages").Scan(&count))
			require.Equal(t, 1, count)
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "document_pages", applied[9].Name)
		})
	}
}

func TestDocumentSHA256Schema(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"snapshot", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			var db database.Database
			var orgID int
			if mode == "snapshot" {
				db = testutil.SetupEmptyTestDB(t)
				require.NoError(t, database.Initialize(ctx, db, postgres.Migrator{}))
				orgID = testutil.CreateTestOrg(t, db, "hash-schema", "Documents")
			} else {
				db = setupMigrationBaseline(t)
				old := fstest.MapFS{}
				schema := os.DirFS("schema")
				names, err := fs.Glob(schema, "migrations/*.sql")
				require.NoError(t, err)
				for _, name := range append(names, "_setup.sql") {
					if path.Dir(name) == "migrations" && path.Base(name) >= "000010" {
						continue
					}
					data, err := fs.ReadFile(schema, name)
					require.NoError(t, err)
					old[name] = &fstest.MapFile{Data: data}
				}
				require.NoError(t, (postgres.Migrator{}).Migrate(ctx, db, old, []string{"users.sql"}))
				applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
				require.NoError(t, err)
				require.Contains(t, applied, 9)
				require.NotContains(t, applied, 10)
				require.NoError(t, db.QueryRow(ctx, "INSERT INTO organizations DEFAULT VALUES RETURNING id").Scan(&orgID))
			}

			hash := strings.Repeat("0123456789abcdef", 4)
			for _, tt := range []struct {
				name string
				key  string
				want string
			}{
				{name: "canonical PDF", key: "documents/%s/pdf/" + hash, want: hash},
				{name: "no PDF"},
				{name: "legacy PDF", key: "documents/%s/pdf"},
				{name: "source object", key: "documents/%s/original/" + hash},
				{name: "another document", key: "documents/00000000-0000-0000-0000-000000000000/pdf/" + hash},
				{name: "uppercase hash", key: "documents/%s/pdf/" + strings.ToUpper(hash)},
				{name: "short hash", key: "documents/%s/pdf/" + hash[:63]},
				{name: "long hash", key: "documents/%s/pdf/" + hash + "0"},
				{name: "nonhex hash", key: "documents/%s/pdf/" + hash[:63] + "g"},
				{name: "extra path", key: "documents/%s/pdf/" + hash + "/file.pdf"},
			} {
				var id int
				var externalID string
				require.NoError(t, db.QueryRow(ctx, `
					INSERT INTO documents(organization_id, filename, content_type, size, object_key, updated_at)
					VALUES ($1, 'letter.pdf', 'application/pdf', 42, $2, '2000-01-01T00:00:00Z')
					RETURNING id, external_id`, orgID, tt.name).Scan(&id, &externalID))
				_, err := db.Exec(ctx, "UPDATE documents SET pdf_key = $1 WHERE id = $2", strings.ReplaceAll(tt.key, "%s", externalID), id)
				require.NoError(t, err)
				if mode == "snapshot" {
					_, err = db.Exec(ctx, "UPDATE documents SET sha256 = $1 WHERE id = $2", tt.want, id)
					require.NoError(t, err)
				}
			}

			for range 2 {
				require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
				rows, err := db.Query(ctx, "SELECT object_key, sha256, updated_at FROM documents ORDER BY id")
				require.NoError(t, err)
				require.NoError(t, database.ScanRows(rows, func(row database.Row) error {
					var key, got string
					var updatedAt time.Time
					if err := row.Scan(&key, &got, &updatedAt); err != nil {
						return err
					}
					if key == "canonical PDF" {
						require.Equal(t, hash, got)
					} else {
						require.Empty(t, got, key)
					}
					require.True(t, updatedAt.Equal(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)))
					return nil
				}))
			}
			var got string
			require.NoError(t, db.QueryRow(ctx, `
				INSERT INTO documents(organization_id, filename, content_type, size, object_key)
				VALUES ($1, 'new.pdf', 'application/pdf', 42, 'new') RETURNING sha256`, orgID).Scan(&got))
			require.Empty(t, got)
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "document_sha256", applied[10].Name)
		})
	}
}

func TestUploadRecoverySchema(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"snapshot", "upgrade"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			db := testutil.SetupEmptyTestDB(t)
			require.NoError(t, database.Initialize(ctx, db, postgres.Migrator{}))
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Recovery")
			if mode == "upgrade" {
				_, err := db.Exec(ctx, `ALTER TABLE documents DROP COLUMN upload_token, DROP COLUMN upload_expires_at, DROP COLUMN upload_ready`)
				require.NoError(t, err)
				_, err = db.Exec(ctx, `DROP INDEX idx_documents_upload_recovery`)
				require.NoError(t, err)
				_, err = db.Exec(ctx, `DELETE FROM __migrations WHERE id = 11`)
				require.NoError(t, err)
			}
			_, err := db.Exec(ctx, `INSERT INTO documents(organization_id, filename, content_type, size, object_key) VALUES ($1, 'scan.pdf', 'application/pdf', 0, 'recovery')`, orgID)
			require.NoError(t, err)
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			var token, status string
			var ready bool
			var expiry time.Time
			require.NoError(t, db.QueryRow(ctx, `SELECT upload_token, upload_expires_at, upload_ready, status FROM documents WHERE organization_id = $1`, orgID).Scan(&token, &expiry, &ready, &status))
			require.NotEmpty(t, token)
			require.False(t, ready)
			require.True(t, expiry.After(time.Now()))
			require.Equal(t, "uploading", status)
			require.NoError(t, database.Migrate(ctx, db, postgres.Migrator{}))
			applied, err := (postgres.Migrator{}).GetAppliedMigrations(ctx, db)
			require.NoError(t, err)
			require.Equal(t, "upload_recovery", applied[11].Name)
			var index string
			require.NoError(t, db.QueryRow(ctx, `SELECT indexdef FROM pg_indexes WHERE indexname = 'idx_documents_upload_recovery' AND schemaname = 'public'`).Scan(&index))
			require.Contains(t, index, "(id)")
			require.Contains(t, index, "uploading")
		})
	}
}
