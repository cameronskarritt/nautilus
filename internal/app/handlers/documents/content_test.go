package documents_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/app/handlers/documents"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentContent(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"viewer", "read API key", "HTML attachment", "empty", "maximum", "other tenant", "pending", "missing session", "write API key", "missing encryptor", "wrong encryptor", "wrong document binding", "wrong organization binding", "tampered", "size mismatch", "oversized object", "read failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Documents")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			ctx := organizations.WithContext(t.Context(), org)
			ctx = users.WithContext(ctx, &users.User{ID: 1})
			ctx = sessions.WithContext(ctx, 1)
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: organizations.RoleMember})
			enc := encrypt.ForOrganization(contentKeys{}, org.ExternalID)
			ctx = encrypt.WithContext(ctx, enc)
			store := &contentStore{}
			router := mux.New(mux.Config{})
			documents.NewMux(db, store).Mount(router, "/documents")
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
			var body bytes.Buffer
			writer := multipart.NewWriter(&body)
			part, err := writer.CreateFormFile("file", "lettre été.txt")
			require.NoError(t, err)
			_, err = part.Write(data)
			require.NoError(t, err)
			require.NoError(t, writer.Close())
			upload := httptest.NewRequest(http.MethodPost, "/documents", &body).WithContext(ctx)
			upload.Header.Set("Content-Type", writer.FormDataContentType())
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, upload)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			var response struct {
				Document struct {
					ID          string `json:"id"`
					ContentType string `json:"content_type"`
				} `json:"document"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			id := response.Document.ID
			ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: organizations.RoleViewer})
			status, code := http.StatusOK, ""
			switch name {
			case "read API key":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
			case "other tenant":
				otherID := testutil.CreateTestOrg(t, db, "other", "Other")
				other, err := organizations.Get(t.Context(), db, otherID)
				require.NoError(t, err)
				ctx = organizations.WithContext(ctx, other)
				ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: other.ID, Role: organizations.RoleViewer})
				ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(contentKeys{}, other.ExternalID))
				status, code = http.StatusNotFound, "HTTP-404"
			case "pending":
				_, err = db.Exec(ctx, "UPDATE documents SET status = 'pending' WHERE external_id = $1", id)
				require.NoError(t, err)
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
			rec = httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/documents/"+id+"/content", nil).WithContext(ctx))
			require.Equal(t, status, rec.Code)
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			if status == http.StatusOK {
				require.Equal(t, string(data), rec.Body.String())
				require.Equal(t, response.Document.ContentType, rec.Header().Get("Content-Type"))
				require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
				disposition, params, err := mime.ParseMediaType(rec.Header().Get("Content-Disposition"))
				require.NoError(t, err)
				require.Equal(t, "attachment", disposition)
				require.Equal(t, "lettre été.txt", params["filename"])
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
