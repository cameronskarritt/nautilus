package database

import (
	"context"

	"nautilus/internal/errors"
	"nautilus/internal/log"
)

type Database interface {
	Begin(context.Context) (Transaction, error)
	Exec(ctx context.Context, query string, args ...any) (Result, error)
	Query(ctx context.Context, query string, args ...any) (Rows, error)
	QueryRow(ctx context.Context, query string, args ...any) Row
}

type Transaction interface {
	Database
	Commit(context.Context) error
	Rollback(context.Context) error
}

type Locker interface {
	Lock(ctx context.Context, name string) error
	Unlock(ctx context.Context, name string) error
}

type closer interface {
	Close() error
}

func Close(ctx context.Context, db Database) func() {
	noop := func() {}
	if db == nil {
		return noop
	}

	c, ok := db.(closer)
	if !ok {
		return noop
	}

	return func() {
		logger := log.FromContext(ctx)
		logger.Debug("closing database")
		err := c.Close()
		if err != nil {
			logger.Error("error closing database", "error", err)
			return
		}
		logger.Debug("database closed")
	}
}

func Transact(ctx context.Context, db Database, txFunc func(Database) error) (err error) {
	tx, err := db.Begin(ctx)
	if err != nil {
		return errors.Wrap(err, "error beginning transaction")
	}

	defer func() {
		logger := log.FromContext(ctx)
		if r := recover(); r != nil {
			e := tx.Rollback(ctx)
			if e != nil {
				logger.Error("error rolling back transaction", "error", e)
			}
			panic(r)
		} else if err != nil {
			e := tx.Rollback(ctx)
			if e != nil {
				logger.Error("error rolling back transaction", "error", e)
			}
			err = errors.Wrap(err, "error performing transaction")
		} else {
			err = tx.Commit(ctx)
			if err != nil {
				err = errors.Wrap(err, "error committing transaction")
			}
		}
	}()

	err = txFunc(tx)
	return err
}
