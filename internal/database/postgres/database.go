package postgres

import (
	"context"
	"hash/fnv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"nautilus/internal/database"
	"nautilus/internal/errors"
)

var (
	_ database.Database = new(pgxDB)
	_ database.Locker   = new(pgxDB)
)

type pgxDB struct {
	pool *pgxpool.Pool
}

type pgxTx struct {
	pgx.Tx
}

type pgxRows struct {
	pgx.Rows
}

func Connect(ctx context.Context, url string) (*pgxDB, error) {
	if url == "" {
		return nil, errors.New("DATABASE_URL is required")
	}
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create connection pool")
	}

	return &pgxDB{pool: pool}, nil
}

func (db *pgxDB) Close() {
	db.pool.Close()
}

func (r *pgxRows) Close() error {
	r.Rows.Close()
	err := r.Err()
	if err != nil {
		return errors.Wrap(err, "unable to close rows")
	}
	return nil
}

func (tx *pgxTx) Begin(ctx context.Context) (database.Transaction, error) {
	ttx, err := tx.Tx.Begin(ctx)
	if err != nil {
		return nil, errors.Wrap(err, "unable to begin transaction")
	}

	return &pgxTx{Tx: ttx}, nil
}

func (tx *pgxTx) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	tag, err := tx.Tx.Exec(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to execute query")
	}

	return tag, nil
}

func (tx *pgxTx) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	rows, err := tx.Tx.Query(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to query database")
	}

	return &pgxRows{Rows: rows}, nil
}

func (tx *pgxTx) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return tx.Tx.QueryRow(ctx, query, args...)
}

func (db *pgxDB) Begin(ctx context.Context) (database.Transaction, error) {
	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to begin transaction")
	}

	return &pgxTx{Tx: tx}, nil
}

func (db *pgxDB) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	rows, err := db.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to query database")
	}

	return &pgxRows{Rows: rows}, nil
}

func (db *pgxDB) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	return db.pool.QueryRow(ctx, query, args...)
}

func (db *pgxDB) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	tag, err := db.pool.Exec(ctx, query, args...)
	if err != nil {
		return nil, errors.Wrap(err, "unable to execute query")
	}

	return tag, nil
}

// Lock acquires a PostgreSQL advisory lock using a hash of the name.
// Advisory locks are session-level and persist until explicitly released
// or the connection is closed.
func (db *pgxDB) Lock(ctx context.Context, name string) error {
	lockID := hashLockName(name)
	_, err := db.pool.Exec(ctx, "SELECT pg_advisory_lock($1)", lockID)
	if err != nil {
		return errors.Wrapf(err, "unable to acquire lock: %s", name)
	}
	return nil
}

// Unlock releases a previously acquired PostgreSQL advisory lock.
func (db *pgxDB) Unlock(ctx context.Context, name string) error {
	lockID := hashLockName(name)
	_, err := db.pool.Exec(ctx, "SELECT pg_advisory_unlock($1)", lockID)
	if err != nil {
		return errors.Wrapf(err, "unable to release lock: %s", name)
	}
	return nil
}

// hashLockName converts a string lock name to an int64 for use with pg_advisory_lock.
func hashLockName(name string) int64 {
	h := fnv.New64a()
	h.Write([]byte(name))
	return int64(h.Sum64())
}
