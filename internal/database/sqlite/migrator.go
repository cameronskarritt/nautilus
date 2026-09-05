package sqlite

import (
	"context"
	"io/fs"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

var _ database.Migrator = Migrator{}

type Migrator struct{}

func (m Migrator) Initialize(
	ctx context.Context,
	db database.Database,
	schema fs.FS,
	schemaFiles []string,
) error {
	return database.InitializeSchema(ctx, db, m, schema, schemaFiles)
}

func (m Migrator) Migrate(
	ctx context.Context,
	db database.Database,
	schema fs.FS,
	schemaFiles []string,
) error {
	return database.MigrateSchema(ctx, db, m, schema, schemaFiles)
}

func (Migrator) SchemaInitialized(
	ctx context.Context,
	db database.Database,
	table string,
) (bool, error) {
	query := `
		SELECT EXISTS (
			SELECT 1
			FROM sqlite_master
			WHERE type = 'table'
				AND name = ?
		);
	`

	var initialized bool
	err := db.QueryRow(ctx, query, table).Scan(&initialized)
	return initialized, err
}

func (Migrator) GetAppliedMigrations(
	ctx context.Context,
	db database.Database,
) (map[int]database.MigrationRecord, error) {
	records := make(map[int]database.MigrationRecord)
	rows, err := db.Query(ctx, `SELECT id, name, checksum, ran_at FROM __migrations;`)
	if err != nil {
		return nil, errors.Wrap(err, "unable to query migrations")
	}
	err = database.ScanRows(rows, func(row database.Row) error {
		var record database.MigrationRecord
		if err := row.Scan(&record.ID, &record.Name, &record.Checksum, &record.RanAt); err != nil {
			return errors.Wrap(err, "unable to scan migration")
		}
		records[record.ID] = record
		return nil
	})
	return records, err
}

func (Migrator) RecordMigration(
	ctx context.Context,
	db database.Database,
	record database.MigrationRecord,
) error {
	_, err := db.Exec(
		ctx,
		`INSERT INTO __migrations(id, name, checksum) VALUES (?, ?, ?);`,
		record.ID,
		record.Name,
		record.Checksum,
	)
	return err
}
