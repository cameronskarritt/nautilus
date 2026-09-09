package documents

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
	"nautilus/internal/scan"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentContent(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"generated PDF", "viewer", "read API key", "HTML attachment", "empty", "maximum", "other tenant", "uploading", "failed", "missing session", "write API key", "missing encryptor", "wrong encryptor", "wrong document binding", "wrong organization binding", "tampered", "size mismatch", "oversized object", "read failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Documents")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			ctx := organizations.WithContext(t.Context(), org)
			ctx = users.WithContext(ctx, &users.User{ID: 1})
			ctx = sessions.WithContext(ctx, 1)
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: enums.RoleMember})
			enc := encrypt.ForOrganization(contentKeys{}, org.ExternalID)
			ctx = encrypt.WithContext(ctx, enc)
			store := &contentStore{}
			router := mux.New(mux.Config{})
			NewMux(db, store, nil).Mount(router, "/documents")
			data := []byte("synthetic private correspondence")
			if name == "HTML attachment" {
				data = []byte("<!DOCTYPE html><script>alert('synthetic')</script>")
			}
			if name == "empty" {
				data = nil
			}
			if name == "maximum" {
				data = bytes.Repeat([]byte{'x'}, encrypt.MaxPlaintextSize)
			}
			opts := &documents.CreateOptions{Filename: "lettre été.txt", ContentType: http.DetectContentType(data), Size: int64(len(data))}
			if name == "generated PDF" {
				image := uploadImage(t, "png")
				data, err = scan.PDF(ctx, [][]byte{image, image})
				require.NoError(t, err)
				opts = &documents.CreateOptions{Filename: "lettre été.pdf", ContentType: "application/pdf", Pages: []documents.PageOptions{{ContentType: "image/png", Size: int64(len(image))}, {ContentType: "image/png", Size: int64(len(image))}}}
			}
			doc, err := documents.Create(ctx, db, org.ID, opts)
			require.NoError(t, err)
			id := doc.ExternalID
			if name == "generated PDF" {
				hash := sha256.Sum256(data)
				doc, err = documents.PublishPDF(ctx, db, org.ID, id, doc.ObjectKey+"/pdf/"+hex.EncodeToString(hash[:]), int64(len(data)))
				require.NoError(t, err)
				require.NotNil(t, doc)
			}
			store.data, err = enc.Seal(ctx, data, encrypt.Binding{Purpose: "document", RecordID: id})
			require.NoError(t, err)
			state := enums.DocumentStatusUploaded
			if name == "uploading" {
				state = enums.DocumentStatusUploading
			}
			if name == "failed" {
				state = enums.DocumentStatusFailed
			}
			_, err = db.Exec(ctx, "UPDATE documents SET status = $1 WHERE external_id = $2 AND organization_id = $3", state, id, org.ID)
			require.NoError(t, err)
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: enums.RoleViewer})
			status, code := http.StatusOK, ""
			switch name {
			case "read API key":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
			case "other tenant":
				otherID := testutil.CreateTestOrg(t, db, "other", "Other")
				other, err := organizations.Get(t.Context(), db, otherID)
				require.NoError(t, err)
				ctx = organizations.WithContext(ctx, other)
				ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: other.ID, Role: enums.RoleViewer})
				ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(contentKeys{}, other.ExternalID))
				status, code = http.StatusNotFound, "HTTP-404"
			case "uploading", "failed":
				status, code = http.StatusNotFound, "HTTP-404"
			case "missing session":
				ctx = sessions.WithContext(ctx, 0)
				status, code = http.StatusForbidden, "DOC-02"
			case "write API key":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
				status, code = http.StatusForbidden, "DOC-02"
			case "missing encryptor":
				ctx = encrypt.WithContext(ctx, nil)
				status, code = http.StatusForbidden, "DOC-02"
			case "wrong encryptor":
				ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(contentKeys{}, "other"))
				status, code = http.StatusForbidden, "DOC-02"
			case "wrong document binding":
				store.data, err = enc.Seal(ctx, data, encrypt.Binding{Purpose: "document", RecordID: "other"})
				require.NoError(t, err)
				status, code = http.StatusInternalServerError, "HTTP-500"
			case "wrong organization binding":
				store.data, err = encrypt.ForOrganization(contentKeys{}, "other").Seal(ctx, data, encrypt.Binding{Purpose: "document", RecordID: id})
				require.NoError(t, err)
				status, code = http.StatusInternalServerError, "HTTP-500"
			case "tampered":
				store.data[len(store.data)-1] ^= 1
				status, code = http.StatusInternalServerError, "HTTP-500"
			case "size mismatch":
				_, err = db.Exec(ctx, "UPDATE documents SET size = size + 1 WHERE external_id = $1", id)
				require.NoError(t, err)
				status, code = http.StatusInternalServerError, "HTTP-500"
			case "oversized object":
				store.data = bytes.Repeat([]byte{'x'}, encrypt.MaxPlaintextSize+128<<10)
				status, code = http.StatusInternalServerError, "HTTP-500"
			case "read failure":
				store.readErr = true
				status, code = http.StatusInternalServerError, "HTTP-500"
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/"+id+"/content", nil).WithContext(ctx))
			require.Equal(t, status, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if status == http.StatusOK {
				require.Equal(t, string(data), rec.Body.String())
				require.Equal(t, doc.ContentType, rec.Header().Get("Content-Type"))
				require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
				disposition, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
				require.NoError(t, err)
				require.Equal(t, "attachment", disposition)
				require.Equal(t, doc.Filename, params["filename"])
				key := doc.ObjectKey
				if doc.PageCount > 0 {
					key = doc.PDFKey
				}
				require.Equal(t, key, store.key)
			} else {
				require.Contains(t, rec.Body.String(), code)
				require.NotContains(t, rec.Body.String(), "synthetic private")
				require.Empty(t, rec.Header().Get("Content-Disposition"))
			}
			if status == http.StatusForbidden || status == http.StatusNotFound {
				require.Zero(t, store.gets)
			} else {
				require.Equal(t, 1, store.gets)
				require.True(t, store.closed)
			}
			if name == "oversized object" {
				require.Less(t, store.read, len(store.data))
			}
		})
	}
}

type contentKeys struct{}

func (contentKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{7}, 32), nil
}
func (contentKeys) UserKey(context.Context) ([]byte, error) {
	return nil, errors.New("unexpected user key")
}

type contentStore struct {
	objectstore.Store
	data    []byte
	key     string
	gets    int
	closed  bool
	read    int
	readErr bool
}

func (s *contentStore) Put(_ context.Context, _ string, body io.Reader, _ *objectstore.PutOptions) error {
	var err error
	s.data, err = io.ReadAll(body)
	if err != nil {
		return errors.Wrap(err, "unable to capture document upload")
	}
	return nil
}
func (s *contentStore) Get(_ context.Context, key string, _ *objectstore.GetOptions) (*objectstore.Object, error) {
	s.gets++
	s.key = key
	if !strings.HasPrefix(key, "documents/") {
		return nil, objectstore.ErrNotFound
	}
	return &objectstore.Object{Body: s}, nil
}
func (s *contentStore) Read(p []byte) (int, error) {
	if s.readErr {
		return 0, io.ErrUnexpectedEOF
	}
	if s.read == len(s.data) {
		return 0, io.EOF
	}
	n := copy(p, s.data[s.read:])
	s.read += n
	return n, nil
}
func (s *contentStore) Close() error { s.closed = true; return nil }
