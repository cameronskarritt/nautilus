package documents

import (
	"net/http"
	"testing"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/mux"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestSessionDocumentText(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"viewer", "missing session", "missing member", "missing organization", "wrong organization", "missing encryptor", "wrong encryptor", "deleted organization"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			ctx, org := actor(t, db)
			doc := createDocument(t, db, org.ID, true)
			enc := encrypt.ForOrganization(contentKeys{}, org.ExternalID)
			ctx = encrypt.WithContext(ctx, enc)
			data, err := enc.Seal(ctx, []byte("synthetic text"), encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
			require.NoError(t, err)
			store := &contentStore{data: data}
			status, code := http.StatusForbidden, "DOC-02"
			switch name {
			case "viewer":
				status = http.StatusOK
			case "missing session":
				ctx = sessions.WithContext(ctx, 0)
			case "missing member":
				ctx = organizations.WithMemberContext(ctx, nil)
			case "missing organization":
				ctx = organizations.WithContext(ctx, nil)
				code = "DOC-01"
			case "wrong organization":
				other := *org
				other.ID++
				ctx = organizations.WithContext(ctx, &other)
			case "missing encryptor":
				ctx = encrypt.WithContext(ctx, nil)
			case "wrong encryptor":
				ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(contentKeys{}, "other"))
			case "deleted organization":
				require.NoError(t, organizations.Delete(ctx, db, org.ID))
				status, code = http.StatusNotFound, "HTTP-404"
			}
			router := mux.New(mux.Config{})
			NewMux(db, store, nil).Mount(router, "/documents")
			rec := request(router, ctx, http.MethodGet, "/documents/"+doc.ExternalID+"/text")
			require.Equal(t, status, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if status == http.StatusOK {
				require.Equal(t, "synthetic text", rec.Body.String())
				require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
				require.Equal(t, 1, store.gets)
				require.True(t, store.closed)
			} else {
				require.Contains(t, rec.Body.String(), `"code":"`+code+`"`)
				require.NotContains(t, rec.Body.String(), "synthetic text")
				require.Zero(t, store.gets)
			}
		})
	}
}
