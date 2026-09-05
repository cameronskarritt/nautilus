package database

import (
	"database/sql"

	"nautilus/internal/errors"
)

type Result interface {
	RowsAffected() int64
}

type Row interface {
	Scan(dest ...any) error
}

type Rows interface {
	Row
	Close() error
	Err() error
	Next() bool
}

func ScanRows(rows Rows, scan func(row Row) error) (err error) {
	defer func() {
		if closeErr := rows.Close(); closeErr != nil {
			err = errors.Wrap(closeErr, "error closing rows")
		}
	}()

	for rows.Next() {
		e := scan(rows)
		if e != nil {
			err = e
			break
		}
	}
	if e := rows.Err(); e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			return nil
		}
		return errors.Wrap(e, "error scanning rows")
	}

	return err
}
