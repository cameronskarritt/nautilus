package tracer

import (
	"context"
	"testing"

	"nautilus/internal/ai/llm"
	"nautilus/internal/database"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mail"
	"nautilus/internal/testutil/require"
)

func TestTracedDatabaseExec(t *testing.T) {
	t.Parallel()

	db := &fakeDatabase{}
	tr := &recordingTracer{}
	tdb := NewTracedDatabase(db, tr)

	result, err := tdb.Exec(context.Background(), "insert into users values ($1)", 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), result.RowsAffected())

	span := tr.onlySpan(t)
	require.Equal(t, "db.exec", span.Name)
	require.True(t, span.Ended)
	require.Equal(t, map[string]any{"db.query": "insert into users values ($1)"}, span.Attrs)
	require.Equal(t, StatusUnset, span.Status)
}

func TestTracedDatabaseError(t *testing.T) {
	t.Parallel()

	want := errors.New("query failed")
	db := &fakeDatabase{
		ExecFunc: func(context.Context, string, ...any) (database.Result, error) {
			return nil, want
		},
	}
	tr := &recordingTracer{}
	tdb := NewTracedDatabase(db, tr)

	_, err := tdb.Exec(context.Background(), "bad query")
	require.ErrorIs(t, err, want)

	span := tr.onlySpan(t)
	require.Equal(t, want, span.Err)
	require.Equal(t, StatusError, span.Status)
	require.Equal(t, want.Error(), span.StatusDesc)
}

func TestTracedTransaction(t *testing.T) {
	t.Parallel()

	tx := &fakeTransaction{}
	tr := &recordingTracer{}
	ttx := &TracedTransaction{tx: tx, tracer: tr}

	require.NoError(t, ttx.Commit(context.Background()))
	require.NoError(t, ttx.Rollback(context.Background()))

	require.Equal(t, []string{"db.commit", "db.rollback"}, tr.spanNames())
	require.True(t, tr.Spans[0].Ended)
	require.True(t, tr.Spans[1].Ended)
}

func TestTracedLLMClient(t *testing.T) {
	t.Parallel()

	client := &fakeLLMClient{}
	tr := &recordingTracer{}
	traced := NewTracedLLMClient(client, tr)
	req := &llm.Request{Model: enums.Model("test-model")}

	msg, err := traced.Completion(context.Background(), req)
	require.NoError(t, err)
	require.Equal(t, "ok", msg.Content)

	span := tr.onlySpan(t)
	require.Equal(t, "llm.completion", span.Name)
	require.Equal(t, map[string]any{"llm.model": "test-model"}, span.Attrs)
}

func TestTracedMailSender(t *testing.T) {
	t.Parallel()

	sender := &fakeMailSender{}
	tr := &recordingTracer{}
	traced := NewTracedMailSender(sender, tr)

	err := traced.Send(context.Background(), &mail.Message{Subject: "Welcome"})
	require.NoError(t, err)

	span := tr.onlySpan(t)
	require.Equal(t, "mail.send", span.Name)
	require.Equal(t, map[string]any{"mail.subject": "Welcome"}, span.Attrs)
}

type recordingTracer struct {
	Spans []*recordingSpan
}

func (t *recordingTracer) Start(ctx context.Context, name string, _ ...StartOption) (context.Context, Span) {
	span := &recordingSpan{Name: name, Attrs: make(map[string]any)}
	t.Spans = append(t.Spans, span)
	return ctx, span
}

func (t *recordingTracer) onlySpan(tb testing.TB) *recordingSpan {
	tb.Helper()
	require.Len(tb, t.Spans, 1)
	return t.Spans[0]
}

func (t *recordingTracer) spanNames() []string {
	names := make([]string, len(t.Spans))
	for i, span := range t.Spans {
		names[i] = span.Name
	}
	return names
}

type recordingSpan struct {
	Name       string
	Attrs      map[string]any
	Err        error
	Status     Status
	StatusDesc string
	Ended      bool
}

func (s *recordingSpan) SetAttributes(attrs ...Attribute) {
	for _, attr := range attrs {
		s.Attrs[attr.Key] = attr.Value
	}
}

func (s *recordingSpan) RecordError(err error) {
	s.Err = err
}

func (s *recordingSpan) AddEvent(string, ...EventOption) {}

func (s *recordingSpan) SetStatus(status Status, description string) {
	s.Status = status
	s.StatusDesc = description
}

func (s *recordingSpan) End(...EndOption) {
	s.Ended = true
}

type fakeDatabase struct {
	ExecFunc func(context.Context, string, ...any) (database.Result, error)
}

func (d *fakeDatabase) Begin(context.Context) (database.Transaction, error) {
	return &fakeTransaction{}, nil
}

func (d *fakeDatabase) Exec(ctx context.Context, query string, args ...any) (database.Result, error) {
	if d.ExecFunc != nil {
		return d.ExecFunc(ctx, query, args...)
	}
	return fakeResult(1), nil
}

func (d *fakeDatabase) Query(context.Context, string, ...any) (database.Rows, error) {
	return fakeRows{}, nil
}

func (d *fakeDatabase) QueryRow(context.Context, string, ...any) database.Row {
	return fakeRow{}
}

type fakeTransaction struct {
	fakeDatabase
}

func (t *fakeTransaction) Commit(context.Context) error {
	return nil
}

func (t *fakeTransaction) Rollback(context.Context) error {
	return nil
}

type fakeResult int64

func (r fakeResult) RowsAffected() int64 {
	return int64(r)
}

type fakeRow struct{}

func (fakeRow) Scan(...any) error {
	return nil
}

type fakeRows struct {
	fakeRow
}

func (fakeRows) Close() error { return nil }
func (fakeRows) Err() error   { return nil }
func (fakeRows) Next() bool   { return false }

type fakeLLMClient struct{}

func (c *fakeLLMClient) Completion(context.Context, *llm.Request) (*llm.Message, error) {
	return &llm.Message{Content: "ok"}, nil
}

func (c *fakeLLMClient) StreamCompletion(context.Context, *llm.Request) (llm.TokenStream, error) {
	return nil, nil
}

type fakeMailSender struct{}

func (s *fakeMailSender) Send(context.Context, *mail.Message) error {
	return nil
}
