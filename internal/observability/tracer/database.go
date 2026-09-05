package tracer

import (
	"context"

	"nautilus/internal/database"
)

var _ database.Database = (*TracedDatabase)(nil)
var _ database.Transaction = (*TracedTransaction)(nil)

type TracedDatabase struct {
	db     database.Database
	tracer Tracer
}

func NewTracedDatabase(db database.Database, t Tracer) *TracedDatabase {
	return &TracedDatabase{db: db, tracer: t}
}

func (d *TracedDatabase) Begin(ctx context.Context) (database.Transaction, error) {
	ctx, span := d.tracer.Start(ctx, "db.begin")
	defer span.End()

	tx, err := d.db.Begin(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
		return nil, err
	}

	return &TracedTransaction{tx: tx, tracer: d.tracer}, nil
}

func (d *TracedDatabase) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	ctx, span := d.tracer.Start(ctx, "db.exec")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	result, err := d.db.Exec(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return result, err
}

func (d *TracedDatabase) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	ctx, span := d.tracer.Start(ctx, "db.query")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	rows, err := d.db.Query(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return rows, err
}

func (d *TracedDatabase) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	_, span := d.tracer.Start(ctx, "db.query_row")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	return d.db.QueryRow(ctx, query, args...)
}

type TracedTransaction struct {
	tx     database.Transaction
	tracer Tracer
}

func (t *TracedTransaction) Begin(ctx context.Context) (database.Transaction, error) {
	ctx, span := t.tracer.Start(ctx, "db.begin")
	defer span.End()

	tx, err := t.tx.Begin(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
		return nil, err
	}

	return &TracedTransaction{tx: tx, tracer: t.tracer}, nil
}

func (t *TracedTransaction) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	ctx, span := t.tracer.Start(ctx, "db.exec")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	result, err := t.tx.Exec(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return result, err
}

func (t *TracedTransaction) Query(ctx context.Context, query string, args ...any) (database.Rows, error) {
	ctx, span := t.tracer.Start(ctx, "db.query")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	rows, err := t.tx.Query(ctx, query, args...)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return rows, err
}

func (t *TracedTransaction) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	_, span := t.tracer.Start(ctx, "db.query_row")
	defer span.End()

	span.SetAttributes(StringAttr("db.query", query))

	return t.tx.QueryRow(ctx, query, args...)
}

func (t *TracedTransaction) Commit(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "db.commit")
	defer span.End()

	err := t.tx.Commit(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return err
}

func (t *TracedTransaction) Rollback(ctx context.Context) error {
	ctx, span := t.tracer.Start(ctx, "db.rollback")
	defer span.End()

	err := t.tx.Rollback(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(StatusError, err.Error())
	}

	return err
}
