package documents_test

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
	"uuid"

	"nautilus/internal/database"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/optional"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentLifecycle(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "lifecycle")
	opts := &documents.CreateOptions{Filename: "  ../private/report.txt  ", ContentType: " Text/Plain; charset=utf-8 ", Size: 123}
	doc, err := documents.Create(t.Context(), db, org.ID, opts)
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Positive(t, doc.ID)
	require.Equal(t, org.ID, doc.OrganizationID)
	require.Equal(t, enums.DocumentStatusUploading, doc.Status)
	require.Equal(t, "../private/report.txt", doc.Filename)
	require.Equal(t, "text/plain", doc.ContentType)
	require.Equal(t, int64(123), doc.Size)
	require.Equal(t, "documents/"+doc.ExternalID, doc.ObjectKey)
	_, err = uuid.Parse(strings.TrimPrefix(doc.ObjectKey, "documents/"))
	require.NoError(t, err)
	require.NotZero(t, doc.CreatedAt)
	require.NotZero(t, doc.UpdatedAt)
	require.Equal(t, "  ../private/report.txt  ", opts.Filename)

	got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, doc, got)
	page, err := documents.List(t.Context(), db, org.ID, pagination.Params{})
	require.NoError(t, err)
	require.NotNil(t, page.Data)
	require.Equal(t, []*documents.Document{doc}, page.Data)
	require.False(t, page.HasMore)
	require.Empty(t, page.NextCursor)

	uploaded, err := documents.MarkUploaded(t.Context(), db, org.ID, strings.ToUpper(doc.ExternalID))
	require.NoError(t, err)
	require.NotNil(t, uploaded)
	require.Equal(t, enums.DocumentStatusUploaded, uploaded.Status)
	require.Equal(t, doc.ID, uploaded.ID)
	require.Equal(t, doc.ObjectKey, uploaded.ObjectKey)
	require.Equal(t, doc.CreatedAt, uploaded.CreatedAt)
	require.False(t, uploaded.UpdatedAt.Before(doc.UpdatedAt))
	got, err = documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, uploaded, got)
	page, err = documents.List(t.Context(), db, org.ID, pagination.Params{})
	require.NoError(t, err)
	require.Equal(t, []*documents.Document{uploaded}, page.Data)
	got, err = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, uploaded, got)

	encoded, err := json.Marshal(uploaded)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Len(t, fields, 8)
	require.Equal(t, float64(0), fields["page_count"])
	require.Equal(t, "uploaded", fields["status"])
	require.Equal(t, uploaded.ExternalID, fields["id"])
	require.Equal(t, uploaded.Filename, fields["filename"])
	require.Equal(t, uploaded.ContentType, fields["content_type"])
	require.Equal(t, float64(123), fields["size"])
	require.Contains(t, fields, "created_at")
	require.Contains(t, fields, "updated_at")

	other, err := documents.Create(t.Context(), db, org.ID, opts)
	require.NoError(t, err)
	require.NotEqual(t, uploaded.ExternalID, other.ExternalID)
	require.NotEqual(t, uploaded.ObjectKey, other.ObjectKey)
}

func TestDocumentOrganizationIsolation(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	first := createOrganization(t, db, "first")
	second := createOrganization(t, db, "second")
	doc := createDocument(t, db, first.ID, false)
	require.NoError(t, documents.MarkFailed(t.Context(), db, second.ID, doc.ExternalID))
	got, err := documents.GetByExternalID(t.Context(), db, first.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, enums.DocumentStatusUploading, got.Status)
	got, err = documents.MarkUploaded(t.Context(), db, second.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Nil(t, got)
	got, err = documents.MarkUploaded(t.Context(), db, first.ID, doc.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, got)
	got, err = documents.GetByExternalID(t.Context(), db, second.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Nil(t, got)
	page, err := documents.List(t.Context(), db, second.ID, pagination.Params{})
	require.NoError(t, err)
	require.Empty(t, page.Data)

	for _, id := range []string{"", "malformed", uuid.New().String()} {
		got, err := documents.GetByExternalID(t.Context(), db, first.ID, id)
		require.NoError(t, err)
		require.Nil(t, got)
		got, err = documents.MarkUploaded(t.Context(), db, first.ID, id)
		require.NoError(t, err)
		require.Nil(t, got)
		require.NoError(t, documents.MarkFailed(t.Context(), db, first.ID, id))
	}
}

func TestDocumentStatusTransitions(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		status  enums.DocumentStatus
		upload  bool
		want    enums.DocumentStatus
		changed bool
	}{
		{name: "complete upload", status: enums.DocumentStatusUploading, upload: true, want: enums.DocumentStatusUploaded, changed: true},
		{name: "retry completion", status: enums.DocumentStatusUploaded, upload: true, want: enums.DocumentStatusUploaded},
		{name: "failed cannot complete", status: enums.DocumentStatusFailed, upload: true, want: enums.DocumentStatusFailed},
		{name: "fail upload", status: enums.DocumentStatusUploading, want: enums.DocumentStatusFailed, changed: true},
		{name: "uploaded cannot fail", status: enums.DocumentStatusUploaded, want: enums.DocumentStatusUploaded},
		{name: "retry failure", status: enums.DocumentStatusFailed, want: enums.DocumentStatusFailed},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org := createOrganization(t, db, "transitions")
			doc := createDocument(t, db, org.ID, false)
			before := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
			_, err := db.Exec(t.Context(), "UPDATE documents SET status = $1, updated_at = $2 WHERE id = $3", tt.status, before, doc.ID)
			require.NoError(t, err)
			if tt.upload {
				got, err := documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
				require.NoError(t, err)
				if tt.status == enums.DocumentStatusFailed {
					require.Nil(t, got)
				} else {
					require.NotNil(t, got)
					require.Equal(t, tt.want, got.Status)
				}
			} else {
				require.NoError(t, documents.MarkFailed(t.Context(), db, org.ID, doc.ExternalID))
			}
			got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.NotNil(t, got)
			require.Equal(t, tt.want, got.Status)
			if tt.changed {
				require.True(t, got.UpdatedAt.After(before))
			} else {
				require.True(t, got.UpdatedAt.Equal(before))
			}
			page, err := documents.List(t.Context(), db, org.ID, pagination.Params{})
			require.NoError(t, err)
			require.Equal(t, []*documents.Document{got}, page.Data)
		})
	}
}

func TestDocumentUnavailableOrganization(t *testing.T) {
	t.Parallel()
	for _, deleted := range []bool{false, true} {
		t.Run(fmt.Sprintf("deleted=%t", deleted), func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org := createOrganization(t, db, "unavailable")
			uploadingID, uploadedID := uuid.New().String(), uuid.New().String()
			if deleted {
				uploadingID = createDocument(t, db, org.ID, false).ExternalID
				uploadedID = createDocument(t, db, org.ID, true).ExternalID
				require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
			} else {
				_, err := db.Exec(t.Context(), "DELETE FROM organizations WHERE id = $1", org.ID)
				require.NoError(t, err)
			}
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "file", ContentType: "text/plain"})
			require.NoError(t, err)
			require.Nil(t, doc)
			doc, err = documents.MarkUploaded(t.Context(), db, org.ID, uploadingID)
			require.NoError(t, err)
			require.Nil(t, doc)
			require.NoError(t, documents.MarkFailed(t.Context(), db, org.ID, uploadingID))
			doc, err = documents.GetByExternalID(t.Context(), db, org.ID, uploadedID)
			require.NoError(t, err)
			require.Nil(t, doc)
			page, err := documents.List(t.Context(), db, org.ID, pagination.Params{})
			require.NoError(t, err)
			require.Empty(t, page.Data)
			var count int
			require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM documents WHERE organization_id = $1", org.ID).Scan(&count))
			if deleted {
				require.Equal(t, 2, count)
				var status enums.DocumentStatus
				require.NoError(t, db.QueryRow(t.Context(), "SELECT status FROM documents WHERE external_id = $1", uploadingID).Scan(&status))
				require.Equal(t, enums.DocumentStatusUploading, status)
			} else {
				require.Zero(t, count)
			}
		})
	}
}

func TestDocumentValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		opts *documents.CreateOptions
		want error
	}{
		{name: "missing options", want: documents.ErrInvalidFilename},
		{name: "empty filename", opts: &documents.CreateOptions{ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "blank filename", opts: &documents.CreateOptions{Filename: " \t\n", ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "invalid UTF-8", opts: &documents.CreateOptions{Filename: "file\xff", ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "filename control", opts: &documents.CreateOptions{Filename: "fi\nle", ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "filename NUL", opts: &documents.CreateOptions{Filename: "fi\x00le", ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "filename too long", opts: &documents.CreateOptions{Filename: strings.Repeat("文", 256), ContentType: "text/plain"}, want: documents.ErrInvalidFilename},
		{name: "empty content type", opts: &documents.CreateOptions{Filename: "file"}, want: documents.ErrInvalidContentType},
		{name: "missing subtype", opts: &documents.CreateOptions{Filename: "file", ContentType: "text"}, want: documents.ErrInvalidContentType},
		{name: "invalid parameter", opts: &documents.CreateOptions{Filename: "file", ContentType: "text/plain;invalid"}, want: documents.ErrInvalidContentType},
		{name: "wildcard type", opts: &documents.CreateOptions{Filename: "file", ContentType: "*/plain"}, want: documents.ErrInvalidContentType},
		{name: "wildcard subtype", opts: &documents.CreateOptions{Filename: "file", ContentType: "text/*"}, want: documents.ErrInvalidContentType},
		{name: "content type too long", opts: &documents.CreateOptions{Filename: "file", ContentType: "text/" + strings.Repeat("a", 251)}, want: documents.ErrInvalidContentType},
		{name: "negative size", opts: &documents.CreateOptions{Filename: "file", ContentType: "text/plain", Size: -1}, want: documents.ErrInvalidSize},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc, err := documents.Create(t.Context(), nil, 1, tt.opts)
			require.ErrorIs(t, err, tt.want)
			require.Nil(t, doc)
		})
	}
	for _, orgID := range []int{0, -1} {
		_, err := documents.Create(t.Context(), nil, orgID, nil)
		require.ErrorIs(t, err, documents.ErrInvalidOrganization)
		_, err = documents.MarkUploaded(t.Context(), nil, orgID, uuid.New().String())
		require.ErrorIs(t, err, documents.ErrInvalidOrganization)
		require.ErrorIs(t, documents.MarkFailed(t.Context(), nil, orgID, uuid.New().String()), documents.ErrInvalidOrganization)
		_, err = documents.GetByExternalID(t.Context(), nil, orgID, uuid.New().String())
		require.ErrorIs(t, err, documents.ErrInvalidOrganization)
		_, err = documents.List(t.Context(), nil, orgID, pagination.Params{})
		require.ErrorIs(t, err, documents.ErrInvalidOrganization)
	}
}

func TestDocumentMetadataBoundaries(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "boundaries")
	opts := &documents.CreateOptions{Filename: strings.Repeat("文", 255), ContentType: "text/" + strings.Repeat("a", 250)}
	doc, err := documents.Create(t.Context(), db, org.ID, opts)
	require.NoError(t, err)
	require.Equal(t, opts.Filename, doc.Filename)
	require.Equal(t, opts.ContentType, doc.ContentType)
	require.Zero(t, doc.Size)
}

func TestDocumentPagination(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "pagination")
	other := createOrganization(t, db, "other")
	var want []*documents.Document
	for i := range 5 {
		doc := createDocument(t, db, org.ID, i%3 == 1)
		if i%3 == 2 {
			require.NoError(t, documents.MarkFailed(t.Context(), db, org.ID, doc.ExternalID))
			var err error
			doc, err = documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
		}
		want = append([]*documents.Document{doc}, want...)
		createDocument(t, db, other.ID, true)
	}
	page, err := documents.List(t.Context(), db, org.ID, pagination.Params{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, want[:2], page.Data)
	require.True(t, page.HasMore)
	cursor, err := pagination.Decode(page.NextCursor)
	require.NoError(t, err)
	require.Equal(t, pagination.Cursor{"id": strconv.Itoa(want[1].ID), "organization_id": strconv.Itoa(org.ID)}, cursor)
	_, err = documents.List(t.Context(), db, other.ID, pagination.Params{Limit: 2, Cursor: cursor})
	require.ErrorIs(t, err, documents.ErrInvalidCursor)
	// A new upload between pages cannot duplicate or shift older records.
	createDocument(t, db, org.ID, true)
	page, err = documents.List(t.Context(), db, org.ID, pagination.Params{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Equal(t, want[2:4], page.Data)
	require.True(t, page.HasMore)
	cursor, err = pagination.Decode(page.NextCursor)
	require.NoError(t, err)
	page, err = documents.List(t.Context(), db, org.ID, pagination.Params{Limit: 2, Cursor: cursor})
	require.NoError(t, err)
	require.Equal(t, want[4:], page.Data)
	require.False(t, page.HasMore)
	require.Empty(t, page.NextCursor)
	page, err = documents.List(t.Context(), db, org.ID, pagination.Params{Cursor: pagination.Cursor{"id": "1", "organization_id": strconv.Itoa(org.ID)}})
	require.NoError(t, err)
	require.Empty(t, page.Data)
}

func TestDocumentListLimits(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "limits")
	for range 101 {
		createDocument(t, db, org.ID, true)
	}
	for _, tt := range []struct{ limit, want int }{
		{limit: 0, want: 50},
		{limit: -1, want: 50},
		{limit: 1, want: 1},
		{limit: 100, want: 100},
		{limit: 1000, want: 100},
	} {
		page, err := documents.List(t.Context(), db, org.ID, pagination.Params{Limit: tt.limit})
		require.NoError(t, err)
		require.Len(t, page.Data, tt.want)
		require.True(t, page.HasMore)
	}
}

func TestDocumentCursorValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		cursor pagination.Cursor
	}{
		{name: "empty", cursor: pagination.Cursor{}},
		{name: "missing id", cursor: pagination.Cursor{"organization_id": "1"}},
		{name: "missing organization", cursor: pagination.Cursor{"id": "1"}},
		{name: "extra field", cursor: pagination.Cursor{"id": "1", "organization_id": "1", "other": "1"}},
		{name: "numeric id", cursor: pagination.Cursor{"id": float64(1), "organization_id": "1"}},
		{name: "numeric organization", cursor: pagination.Cursor{"id": "1", "organization_id": float64(1)}},
		{name: "different organization", cursor: pagination.Cursor{"id": "1", "organization_id": "2"}},
		{name: "null id", cursor: pagination.Cursor{"id": nil, "organization_id": "1"}},
		{name: "zero id", cursor: pagination.Cursor{"id": "0", "organization_id": "1"}},
		{name: "negative id", cursor: pagination.Cursor{"id": "-1", "organization_id": "1"}},
		{name: "overflow", cursor: pagination.Cursor{"id": "18446744073709551616", "organization_id": "1"}},
		{name: "fractional id", cursor: pagination.Cursor{"id": "1.1", "organization_id": "1"}},
		{name: "padded id", cursor: pagination.Cursor{"id": "01", "organization_id": "1"}},
		{name: "signed id", cursor: pagination.Cursor{"id": "+1", "organization_id": "1"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := documents.List(t.Context(), nil, 1, pagination.Params{Cursor: tt.cursor})
			require.ErrorIs(t, err, documents.ErrInvalidCursor)
		})
	}
}

func TestConcurrentDocumentUploaded(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDBWithCommit(t)
	org := createOrganization(t, db, "concurrent")
	doc := createDocument(t, db, org.ID, false)
	const n = 8
	results := make([]*documents.Document, n)
	errs := make([]error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
		})
	}
	close(start)
	wg.Wait()
	for i := range n {
		require.NoError(t, errs[i])
		require.NotNil(t, results[i])
		require.Equal(t, results[0], results[i])
	}
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.Equal(t, enums.DocumentStatusUploaded, got.Status)
}

func createOrganization(t *testing.T, db database.Database, suffix string) *organizations.Organization {
	t.Helper()
	org, err := organizations.Create(t.Context(), db, t.Name()+suffix, suffix, false, optional.Empty[organizations.Settings]())
	require.NoError(t, err)
	return org
}

func createDocument(t *testing.T, db database.Database, orgID int, uploaded bool) *documents.Document {
	t.Helper()
	doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "document.txt", ContentType: "text/plain", Size: 42})
	require.NoError(t, err)
	require.NotNil(t, doc)
	if uploaded {
		doc, err = documents.MarkUploaded(t.Context(), db, orgID, doc.ExternalID)
		require.NoError(t, err)
		require.NotNil(t, doc)
	}
	return doc
}
