package sqlite

import (
	"context"
	"database/sql"
	"sync"

	_ "modernc.org/sqlite" // Register sqlite driver

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

var (
	_ database.Database = new(sqliteDB)
	_ database.Locker   = new(sqliteDB)
)

// locks holds in-process mutexes for SQLite locking.
// Since SQLite is typically single-process, we use mutexes instead of
// distributed locks like PostgreSQL advisory locks.
var (
	locksMu sync.Mutex
	locks   = make(map[string]*sync.Mutex)
)

type sqliteDB struct {
	db *sql.DB
}

type sqliteTx struct {
	tx *sql.Tx
}

type sqliteRows struct {
	*sql.Rows
}

type sqliteResult struct {
	result sql.Result
}

func (r *sqliteResult) RowsAffected() int64 {
	affected, err := r.result.RowsAffected()
	if err != nil {
		return -1
	}
	return affected
}

func Connect(ctx context.Context, path string) (*sqliteDB, error) {
	if path == "" {
		return nil, errors.New("sqlite database path is required")
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, errors.Wrap(err, "unable to open sqlite database")
	}

	if err := db.PingContext(ctx); err != nil {
		return nil, errors.Wrap(err, "unable to ping sqlite database")
	}

	return &sqliteDB{db: db}, nil
}

func (r *sqliteRows) Close() error {
	err := r.Rows.Close()
	if err != nil {
		return errors.Wrap(err, "unable to close rows")
	}
	return nil
}

func (tx *sqliteTx) Begin(_ context.Context) (database.Transaction, error) {
	// SQLite doesn't support nested transactions, so we return the same transaction
	return tx, nil
}

func (tx *sqliteTx) Commit(_ context.Context) error {
	err := tx.tx.Commit()
	if err != nil {
		return errors.Wrap(err, "unable to commit transaction")
	}
	return nil
}

func (tx *sqliteTx) Rollback(_ context.Context) error {
	err := tx.tx.Rollback()
	if err != nil {
		return errors.Wrap(err, "unable to rollback transaction")
	}
	return nil
}

func (tx *sqliteTx) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	result, err := tx.tx.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to execute query")
	}

	return &sqliteResult{result: result}, nil
}

func (tx *sqliteTx) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	rows, err := tx.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to query database")
	}

	return &sqliteRows{Rows: rows}, nil
}

func (tx *sqliteTx) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return tx.tx.QueryRowContext(ctx, query, args...)
}

func (db *sqliteDB) Begin(ctx context.Context) (database.Transaction, error) {
	tx, err := db.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, errors.Wrap(err, "unable to begin transaction")
	}

	return &sqliteTx{tx: tx}, nil
}

func (db *sqliteDB) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	rows, err := db.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to query database")
	}

	return &sqliteRows{Rows: rows}, nil
}

func (db *sqliteDB) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return db.db.QueryRowContext(ctx, query, args...)
}

func (db *sqliteDB) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	result, err := db.db.ExecContext(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to execute query")
	}

	return &sqliteResult{result: result}, nil
}

func (db *sqliteDB) Close() error {
	err := db.db.Close()
	if err != nil {
		return errors.Wrap(err, "unable to close database")
	}
	return nil
}

// Lock acquires an in-process mutex for the given name.
// Unlike PostgreSQL advisory locks, this only works within a single process.
func (db *sqliteDB) Lock(_ context.Context, name string) error {
	locksMu.Lock()
	mu, ok := locks[name]
	if !ok {
		mu = &sync.Mutex{}
		locks[name] = mu
	}
	locksMu.Unlock()

	mu.Lock()
	return nil
}

// Unlock releases an in-process mutex for the given name.
func (db *sqliteDB) Unlock(_ context.Context, name string) error {
	locksMu.Lock()
	mu, ok := locks[name]
	locksMu.Unlock()

	if !ok {
		return errors.Errorf("lock not found: %s", name)
	}

	mu.Unlock()
	return nil
}
