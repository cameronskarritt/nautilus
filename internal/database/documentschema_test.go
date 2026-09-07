package database_test

import (
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"nautilus/internal/database"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentsSchemaDefaults(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	ctx := t.Context()
	orgID := testutil.CreateTestOrg(t, db, "document-defaults", "Documents")
	var id, size int
	var externalID, filename, contentType, objectKey, status string
	var createdAt, updatedAt time.Time
	err := db.QueryRow(ctx, `
		INSERT INTO documents(organization_id, filename, content_type, size, object_key)
		VALUES ($1, 'letter.pdf', 'application/pdf', 42, 'documents/test-object')
		RETURNING id, external_id, filename, content_type, size, object_key, status, created_at, updated_at;
	`, orgID).Scan(&id, &externalID, &filename, &contentType, &size, &objectKey, &status, &createdAt, &updatedAt)
	require.NoError(t, err)
	require.Positive(t, id)
	require.NotEmpty(t, externalID)
	require.Equal(t, "letter.pdf", filename)
	require.Equal(t, "application/pdf", contentType)
	require.Equal(t, 42, size)
	require.Equal(t, "documents/test-object", objectKey)
	require.Equal(t, "pending", status)
	require.False(t, createdAt.IsZero())
	require.Equal(t, createdAt, updatedAt)
}

func TestDocumentsSchemaConstraints(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		query string
		code  string
	}{
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
		"size", "object_key", "status", "created_at", "updated_at",
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
		AND indexname = 'idx_documents_organization_status_id';
	`).Scan(&index))
	require.Contains(t, index, "(organization_id, status, id DESC)")
}
