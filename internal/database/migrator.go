package database

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
	"time"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

// MigrationLockName is the lock name used to prevent concurrent migration execution.
const MigrationLockName = "database_migrations"

type Migrator interface {
	Initialize(ctx context.Context, db Database, schema fs.FS, schemaFiles []string) error
	Migrate(ctx context.Context, db Database, schema fs.FS, schemaFiles []string) error
	SchemaInitialized(ctx context.Context, db Database, table string) (bool, error)
	GetAppliedMigrations(ctx context.Context, db Database) (map[int]MigrationRecord, error)
	RecordMigration(ctx context.Context, db Database, record MigrationRecord) error
}

type MigrationRecord struct {
	ID       int
	Name     string
	Checksum string
	RanAt    time.Time
}

type migration struct {
	id       int
	name     string
	filename string
	content  []byte
	checksum string
}

func InitializeSchema(
	ctx context.Context,
	db Database,
	migrator Migrator,
	schema fs.FS,
	schemaFiles []string,
) error {
	if err := runSetup(ctx, db, schema); err != nil {
		return err
	}
	if err := runSchema(ctx, db, schema, schemaFiles); err != nil {
		return err
	}

	migrations, err := loadMigrations(schema)
	if err != nil {
		return err
	}
	applied, err := migrator.GetAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	return recordMigrations(ctx, db, migrator, migrations, applied)
}

func MigrateSchema(
	ctx context.Context,
	db Database,
	migrator Migrator,
	schema fs.FS,
	schemaFiles []string,
) error {
	logger := log.FromContext(ctx)

	if locker, ok := db.(Locker); ok {
		if err := locker.Lock(ctx, MigrationLockName); err != nil {
			return errors.Wrap(err, "unable to acquire migration lock")
		}
		defer func() {
			if err := locker.Unlock(ctx, MigrationLockName); err != nil {
				logger.Error("unable to release migration lock", "error", err)
			}
		}()
	}

	migrations, err := loadMigrations(schema)
	if err != nil {
		return err
	}
	if err := runSetup(ctx, db, schema); err != nil {
		return err
	}

	applied, err := migrator.GetAppliedMigrations(ctx, db)
	if err != nil {
		return err
	}
	if err := verifyMigrations(migrations, applied); err != nil {
		return err
	}

	initialized, err := schemaInitialized(ctx, db, migrator, schemaFiles)
	if err != nil {
		return err
	}
	if !initialized {
		err = Transact(ctx, db, func(tx Database) error {
			if err := runSchema(ctx, tx, schema, schemaFiles); err != nil {
				return err
			}
			return recordMigrations(ctx, tx, migrator, migrations, applied)
		})
		if err != nil {
			return errors.Wrap(err, "unable to initialize schema")
		}
		return nil
	}

	for _, migration := range migrations {
		if _, ok := applied[migration.id]; ok {
			continue
		}

		logger.Info("applying migration", "migration", migration.filename)
		err = Transact(ctx, db, func(tx Database) error {
			if _, err := tx.Exec(ctx, string(migration.content)); err != nil {
				return err
			}
			return migrator.RecordMigration(ctx, tx, MigrationRecord{
				ID:       migration.id,
				Name:     migration.name,
				Checksum: migration.checksum,
			})
		})
		if err != nil {
			return errors.Wrapf(err, "unable to run migration: %s", migration.filename)
		}
		logger.Info("successfully applied migration", "migration", migration.filename)
	}
	return nil
}

func runSetup(ctx context.Context, db Database, schema fs.FS) error {
	setup, err := fs.ReadFile(schema, "_setup.sql")
	if err != nil {
		return errors.Wrap(err, "unable to read setup file")
	}
	if _, err := db.Exec(ctx, string(setup)); err != nil {
		return errors.Wrap(err, "unable to run setup file")
	}
	return nil
}

func runSchema(
	ctx context.Context,
	db Database,
	schema fs.FS,
	schemaFiles []string,
) error {
	for _, filename := range schemaFiles {
		query, err := fs.ReadFile(schema, filename)
		if err != nil {
			return errors.Wrapf(err, "unable to read schema file: %s", filename)
		}
		if _, err := db.Exec(ctx, string(query)); err != nil {
			return errors.Wrapf(err, "unable to run schema file: %s", filename)
		}
	}
	return nil
}

func schemaInitialized(
	ctx context.Context,
	db Database,
	migrator Migrator,
	schemaFiles []string,
) (bool, error) {
	if len(schemaFiles) == 0 {
		return false, errors.New("database schema files are required")
	}

	filename := path.Base(schemaFiles[0])
	table := strings.TrimSuffix(filename, path.Ext(filename))
	initialized, err := migrator.SchemaInitialized(ctx, db, table)
	if err != nil {
		return false, errors.Wrap(err, "unable to check schema state")
	}
	return initialized, nil
}

func loadMigrations(schema fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(schema, "migrations")
	if err != nil {
		return nil, errors.Wrap(err, "unable to read migrations directory")
	}

	filenames := make([]string, 0)
	for _, entry := range entries {
		filename := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(filename, ".sql") {
			continue
		}
		filenames = append(filenames, filename)
	}
	slices.Sort(filenames)

	migrations := make([]migration, 0, len(filenames))
	seenIDs := make(map[int]string)
	for _, filename := range filenames {
		var id int
		var name string
		if _, err := fmt.Sscanf(filename, "%06d_%s", &id, &name); err != nil {
			return nil, errors.Wrapf(err, "invalid migration filename: %s", filename)
		}
		if existing, ok := seenIDs[id]; ok {
			return nil, errors.Errorf(
				"duplicate migration ID %d: %s and %s",
				id,
				existing,
				filename,
			)
		}
		seenIDs[id] = filename

		content, err := fs.ReadFile(schema, path.Join("migrations", filename))
		if err != nil {
			return nil, errors.Wrapf(err, "unable to read migration file: %s", filename)
		}
		migrations = append(migrations, migration{
			id:       id,
			name:     strings.TrimSuffix(name, ".sql"),
			filename: filename,
			content:  content,
			checksum: ComputeChecksum(content),
		})
	}
	return migrations, nil
}

func verifyMigrations(
	migrations []migration,
	applied map[int]MigrationRecord,
) error {
	for _, migration := range migrations {
		record, ok := applied[migration.id]
		if !ok {
			continue
		}
		if record.Checksum != migration.checksum {
			return errors.Errorf(
				"checksum mismatch for migration %s: expected %s, got %s",
				migration.filename,
				record.Checksum,
				migration.checksum,
			)
		}
	}
	return nil
}

func recordMigrations(
	ctx context.Context,
	db Database,
	migrator Migrator,
	migrations []migration,
	applied map[int]MigrationRecord,
) error {
	for _, migration := range migrations {
		if _, ok := applied[migration.id]; ok {
			continue
		}
		err := migrator.RecordMigration(ctx, db, MigrationRecord{
			ID:       migration.id,
			Name:     migration.name,
			Checksum: migration.checksum,
		})
		if err != nil {
			return errors.Wrapf(err, "unable to record migration: %s", migration.filename)
		}
	}
	return nil
}

func ComputeChecksum(content []byte) string {
	hash := sha256.Sum256(content)
	return fmt.Sprintf("%x", hash)
}
