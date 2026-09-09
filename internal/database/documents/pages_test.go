package documents_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"uuid"

	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentPages(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "pages")
	other := createOrganization(t, db, "other")
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: 1}, {ContentType: "image/jpeg", Size: 100 << 20}}})
	require.NoError(t, err)
	require.Equal(t, 2, doc.PageCount)
	published, err := documents.MarkUploaded(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Nil(t, published)
	pages, err := documents.ListPages(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, []documents.Page{{Number: 1, ContentType: "image/png", Size: 1, ObjectKey: doc.ObjectKey + "/pages/1"}, {Number: 2, ContentType: "image/jpeg", Size: 100 << 20, ObjectKey: doc.ObjectKey + "/pages/2"}}, pages)
	encoded, err := json.Marshal(pages[0])
	require.NoError(t, err)
	require.JSONEq(t, `{"number":1,"content_type":"image/png","size":1}`, string(encoded))
	for _, id := range []string{doc.ExternalID, uuid.New().String(), "invalid"} {
		got, err := documents.ListPages(t.Context(), db, other.ID, id)
		require.NoError(t, err)
		require.Empty(t, got)
		published, err := documents.PublishPDF(t.Context(), db, other.ID, id, "documents/"+id+"/pdf/"+strings.Repeat("a", 64), 42)
		require.NoError(t, err)
		require.Nil(t, published)
	}
	require.NoError(t, organizations.Delete(t.Context(), db, org.ID))
	pages, err = documents.ListPages(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Empty(t, pages)
	published, err = documents.PublishPDF(t.Context(), db, org.ID, doc.ExternalID, doc.ObjectKey+"/pdf/"+strings.Repeat("a", 64), 100)
	require.NoError(t, err)
	require.Nil(t, published)
}

func TestPublishPDFState(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, contentType, status string
		legacy, allowed           bool
	}{
		{name: "uploading PDF", contentType: "application/pdf", status: "uploading", allowed: true},
		{name: "uploaded PDF", contentType: "application/pdf", status: "uploaded"},
		{name: "failed PDF", contentType: "application/pdf", status: "failed"},
		{name: "non PDF", contentType: "image/png", status: "uploading"},
		{name: "legacy PDF", contentType: "application/pdf", status: "uploading", legacy: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org := createOrganization(t, db, "publish")
			opts := &documents.CreateOptions{Filename: "file", ContentType: tt.contentType}
			if !tt.legacy {
				opts.Pages = []documents.PageOptions{{ContentType: "image/png", Size: 1}}
			}
			doc, err := documents.Create(t.Context(), db, org.ID, opts)
			require.NoError(t, err)
			_, err = db.Exec(t.Context(), "UPDATE documents SET status = $1 WHERE id = $2", tt.status, doc.ID)
			require.NoError(t, err)
			key := doc.ObjectKey + "/pdf/" + strings.Repeat("a", 64)
			published, err := documents.PublishPDF(t.Context(), db, org.ID, doc.ExternalID, key, 100<<20)
			require.NoError(t, err)
			got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			if !tt.allowed {
				require.Nil(t, published)
				require.Zero(t, got.Size)
				require.Empty(t, got.PDFKey)
				require.Empty(t, got.SHA256)
				require.Equal(t, tt.status, got.Status.String())
				return
			}
			require.Equal(t, published, got)
			require.Equal(t, int64(100<<20), got.Size)
			require.Equal(t, key, got.PDFKey)
			require.Equal(t, strings.Repeat("a", 64), got.SHA256)
			require.Equal(t, doc.ObjectKey, got.ObjectKey)
			require.Equal(t, enums.DocumentStatusUploaded, got.Status)
			encoded, err := json.Marshal(got)
			require.NoError(t, err)
			var fields map[string]any
			require.NoError(t, json.Unmarshal(encoded, &fields))
			require.Len(t, fields, 9)
			require.Equal(t, got.SHA256, fields["sha256"])
			require.NotContains(t, string(encoded), key)
			require.NotContains(t, fields, "pdf_key")
			loser, err := documents.PublishPDF(t.Context(), db, org.ID, doc.ExternalID, doc.ObjectKey+"/pdf/"+strings.Repeat("b", 64), 1)
			require.NoError(t, err)
			require.Nil(t, loser)
			got, err = documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.Equal(t, published, got)
		})
	}
}

func TestConcurrentPublishPDF(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDBWithCommit(t)
	org := createOrganization(t, db, "concurrent-pdf")
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: 1}}})
	require.NoError(t, err)
	const n = 8
	results := make([]*documents.Document, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			results[i], errs[i] = documents.PublishPDF(t.Context(), db, org.ID, doc.ExternalID, fmt.Sprintf("%s/pdf/%064x", doc.ObjectKey, i+1), int64(i+1))
		})
	}
	close(start)
	wg.Wait()
	var winner *documents.Document
	for i, result := range results {
		require.NoError(t, errs[i])
		if result == nil {
			continue
		}
		require.Nil(t, winner, "only one publication can win")
		require.Equal(t, fmt.Sprintf("%s/pdf/%064x", doc.ObjectKey, i+1), result.PDFKey)
		require.Equal(t, int64(i+1), result.Size)
		require.Equal(t, fmt.Sprintf("%064x", i+1), result.SHA256)
		winner = result
	}
	require.NotNil(t, winner)
	got, err := documents.GetByExternalID(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Equal(t, winner, got)
}

func TestPublishPDFValidation(t *testing.T) {
	t.Parallel()
	id := uuid.New().String()
	prefix := "documents/" + id + "/pdf/"
	for _, size := range []int64{-1, 0, 100<<20 + 1} {
		_, err := documents.PublishPDF(t.Context(), nil, 1, id, prefix+strings.Repeat("a", 64), size)
		require.ErrorIs(t, err, documents.ErrInvalidSize)
	}
	for _, key := range []string{"", prefix + strings.Repeat("a", 63), prefix + strings.Repeat("a", 65), prefix + strings.Repeat("A", 64), prefix + strings.Repeat("g", 64), prefix + strings.Repeat("0", 63) + "/", "documents/" + uuid.New().String() + "/pdf/" + strings.Repeat("a", 64)} {
		_, err := documents.PublishPDF(t.Context(), nil, 1, id, key, 1)
		require.ErrorIs(t, err, documents.ErrInvalidPDFKey)
	}
	_, err := documents.PublishPDF(t.Context(), nil, 0, id, prefix+strings.Repeat("a", 64), 1)
	require.ErrorIs(t, err, documents.ErrInvalidOrganization)
}

func TestDocumentPageValidation(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		pages []documents.PageOptions
	}{
		{"too many", make([]documents.PageOptions, 101)},
		{"unsupported type", []documents.PageOptions{{ContentType: "image/gif", Size: 1}}},
		{"empty", []documents.PageOptions{{ContentType: "image/png"}}},
		{"negative", []documents.PageOptions{{ContentType: "image/png", Size: -1}}},
		{"oversize", []documents.PageOptions{{ContentType: "image/png", Size: 100<<20 + 1}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := documents.Create(t.Context(), nil, 1, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: tt.pages})
			require.ErrorIs(t, err, documents.ErrInvalidPages)
			require.Nil(t, got)
		})
	}

}

func TestDocumentPageInsertRollback(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "rollback")
	// Force the second page write to fail after the document and first page exist.
	_, err := db.Exec(t.Context(), "CREATE UNIQUE INDEX test_document_page_size ON document_pages(document_id, size)")
	require.NoError(t, err)
	doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: 1}, {ContentType: "image/png", Size: 1}}})
	require.Error(t, err)
	require.Nil(t, doc)
	var count int
	require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM documents WHERE organization_id = $1", org.ID).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRow(t.Context(), "SELECT count(*) FROM document_pages WHERE organization_id = $1", org.ID).Scan(&count))
	require.Zero(t, count)
}

func TestDocumentMaximumPages(t *testing.T) {
	t.Parallel()
	db := testutil.SetupTestDB(t)
	org := createOrganization(t, db, "maximum-pages")
	opts := &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: make([]documents.PageOptions, 100)}
	for i := range opts.Pages {
		opts.Pages[i] = documents.PageOptions{ContentType: "image/jpeg", Size: 1}
	}
	doc, err := documents.Create(t.Context(), db, org.ID, opts)
	require.NoError(t, err)
	require.Equal(t, 100, doc.PageCount)
	pages, err := documents.ListPages(t.Context(), db, org.ID, doc.ExternalID)
	require.NoError(t, err)
	require.Len(t, pages, 100)
	require.Equal(t, 100, pages[99].Number)
	require.Equal(t, doc.ObjectKey+"/pages/100", pages[99].ObjectKey)
}
