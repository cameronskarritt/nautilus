package upload

import (
	"testing"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/optional"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestSourcePageMetadata(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"zero count", "missing metadata", "too many pages", "total bytes"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			org, err := organizations.Create(t.Context(), db, t.Name(), "source-pages", false, optional.Empty[organizations.Settings]())
			require.NoError(t, err)
			size := int64(1)
			if name == "total bytes" {
				size = maxSourceBytes
			}
			doc, err := documents.Create(t.Context(), db, org.ID, &documents.CreateOptions{Filename: "scan.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: size}, {ContentType: "image/png", Size: size}}})
			require.NoError(t, err)
			switch name {
			case "zero count":
				doc.PageCount = 0
			case "missing metadata":
				doc.PageCount = 3
			case "too many pages":
				doc.PageCount = 101
			}
			// Invalid metadata must stop before accessing object storage or encryption keys.
			images, pages, err := (Activities{DB: db}).sourcePages(t.Context(), doc, nil)
			var appErr *temporal.ApplicationError
			require.ErrorAs(t, err, &appErr)
			require.True(t, appErr.NonRetryable())
			require.Nil(t, images)
			require.Nil(t, pages)
		})
	}
}
