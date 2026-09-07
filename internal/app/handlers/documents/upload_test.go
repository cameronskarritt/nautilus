package documents

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestUploadEncryptedDocument(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		filename string
		data     []byte
		wantName string
		wantType string
		role     organizations.Role
		api      bool
	}{
		{name: "member", filename: " letter.txt ", data: []byte("synthetic private correspondence"), wantName: "letter.txt", wantType: "text/plain", role: organizations.RoleMember},
		{name: "owner", filename: "letter.pdf", data: []byte("%PDF-1.7\nsynthetic document"), wantName: "letter.pdf", wantType: "application/pdf", role: organizations.RoleOwner},
		{name: "admin", filename: `C:\scans\letter.txt`, data: []byte("synthetic letter"), wantName: "letter.txt", wantType: "text/plain", role: organizations.RoleAdmin},
		{name: "API write", filename: "/scans/letter.txt", data: []byte("synthetic letter"), wantName: "letter.txt", wantType: "text/plain", api: true},
		{name: "empty", filename: "empty.bin", wantName: "empty.bin", wantType: "application/octet-stream", role: organizations.RoleMember},
		{name: "maximum", filename: "large.txt", data: bytes.Repeat([]byte{'x'}, encrypt.MaxPlaintextSize), wantName: "large.txt", wantType: "text/plain", role: organizations.RoleMember},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Documents")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			keys := new(uploadKeys)
			ctx := uploadSessionContext(t.Context(), org, tt.role)
			if tt.api {
				ctx = apikeys.WithContext(organizations.WithContext(t.Context(), org), &apikeys.Key{
					ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeWrite},
				})
			}
			ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(keys, org.ExternalID))
			store := &uploadStore{beforePut: func(key string) {
				var status string
				require.NoError(t, db.QueryRow(ctx, "SELECT status FROM documents WHERE object_key = $1", key).Scan(&status))
				require.Equal(t, "pending", status)
			}}
			req := uploadRequest(t, tt.filename, tt.data).WithContext(ctx)
			rec := httptest.NewRecorder()
			(&Mux{db: db, store: store}).Upload(rec, req)
			require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			var response struct {
				Document documents.Document `json:"document"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			doc := response.Document
			require.NotEmpty(t, doc.ExternalID)
			require.Equal(t, tt.wantName, doc.Filename)
			require.Equal(t, tt.wantType, doc.ContentType)
			require.Equal(t, int64(len(tt.data)), doc.Size)
			for _, field := range []string{"object_key", "organization_id", "status"} {
				require.NotContains(t, rec.Body.String(), `"`+field+`"`)
			}
			require.Equal(t, 1, store.puts)
			require.Zero(t, store.deletes)
			require.Equal(t, "documents/"+doc.ExternalID, store.key)
			require.NotNil(t, store.opts)
			require.True(t, store.opts.ContentType.Set)
			require.Equal(t, "application/octet-stream", store.opts.ContentType.Data)
			require.False(t, store.opts.Metadata.Set)
			if len(tt.data) > 0 {
				require.False(t, bytes.Contains(store.data, tt.data))
			}
			binding := encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID}
			plaintext, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, store.data, binding)
			require.NoError(t, err)
			defer clear(plaintext)
			require.True(t, bytes.Equal(tt.data, plaintext))
			for _, wrong := range []encrypt.Binding{
				{Purpose: "totp", RecordID: doc.ExternalID},
				{Purpose: "document", RecordID: "another-document"},
			} {
				_, err = encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, store.data, wrong)
				require.ErrorIs(t, err, encrypt.ErrInvalidEnvelope)
			}
			_, err = encrypt.ForOrganization(keys, "another-organization").Open(ctx, store.data, binding)
			require.ErrorIs(t, err, encrypt.ErrInvalidEnvelope)
			stored, err := documents.GetByExternalID(ctx, db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.NotNil(t, stored)
			require.Equal(t, "ready", stored.Status)
			require.Zero(t, keys.userCalls)
			require.Equal(t, make([]byte, 32), keys.returned)
		})
	}
}

func TestUploadRejectsInvalidBodies(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		build  func(*testing.T) *http.Request
		status int
		code   errors.ErrorCode
	}{
		{name: "unsupported", build: func(t *testing.T) *http.Request {
			t.Helper()
			r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader("synthetic private body"))
			r.Header.Set("Content-Type", "application/json")
			return r
		}, status: http.StatusUnsupportedMediaType, code: errors.ErrorCodeDOC05},
		{name: "missing boundary", build: func(t *testing.T) *http.Request {
			t.Helper()
			r := httptest.NewRequest(http.MethodPost, "/documents", nil)
			r.Header.Set("Content-Type", "multipart/form-data")
			return r
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "malformed", build: func(t *testing.T) *http.Request {
			t.Helper()
			r := httptest.NewRequest(http.MethodPost, "/documents", strings.NewReader("synthetic private malformed body"))
			r.Header.Set("Content-Type", "multipart/form-data; boundary=absent")
			return r
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "missing file", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) {})
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "field instead of file", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) { require.NoError(t, w.WriteField("file", "synthetic private body")) })
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "wrong part name", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) { _, err := w.CreateFormFile("other", "letter.txt"); require.NoError(t, err) })
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "extra field", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) {
				_, err := w.CreateFormFile("file", "letter.txt")
				require.NoError(t, err)
				require.NoError(t, w.WriteField("organization_id", "attacker-organization"))
			})
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "extra file", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) {
				for range 2 {
					_, err := w.CreateFormFile("file", "letter.txt")
					require.NoError(t, err)
				}
			})
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC04},
		{name: "invalid filename", build: func(t *testing.T) *http.Request {
			t.Helper()
			return uploadRequest(t, strings.Repeat("x", 256), nil)
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC07},
		{name: "path without filename", build: func(t *testing.T) *http.Request {
			t.Helper()
			return uploadRequest(t, "..", nil)
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC07},
		{name: "file limit", build: func(t *testing.T) *http.Request {
			t.Helper()
			return uploadRequest(t, "large.txt", bytes.Repeat([]byte{'x'}, encrypt.MaxPlaintextSize+1))
		}, status: http.StatusRequestEntityTooLarge, code: errors.ErrorCodeDOC06},
		{name: "body limit", build: func(t *testing.T) *http.Request {
			t.Helper()
			r := uploadRequest(t, "letter.txt", nil)
			r.Body = io.NopCloser(io.MultiReader(r.Body, bytes.NewReader(make([]byte, maxUploadBody))))
			return r
		}, status: http.StatusRequestEntityTooLarge, code: errors.ErrorCodeDOC06},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			org := &organizations.Organization{ID: 1, ExternalID: "organization"}
			keys := new(uploadKeys)
			ctx := encrypt.WithContext(uploadSessionContext(t.Context(), org, organizations.RoleMember), encrypt.ForOrganization(keys, org.ExternalID))
			var logs bytes.Buffer
			ctx = log.WithContext(ctx, log.New(slog.NewJSONHandler(&logs, nil)))
			store := new(uploadStore)
			rec := httptest.NewRecorder()
			(&Mux{store: store}).Upload(rec, tt.build(t).WithContext(ctx))
			require.Equal(t, tt.status, rec.Code)
			requireUploadError(t, rec, tt.code)
			require.Zero(t, keys.orgCalls)
			require.Zero(t, store.puts)
			require.NotContains(t, rec.Body.String(), "synthetic private")
			require.NotContains(t, logs.String(), "synthetic private")
		})
	}
}

func TestUploadAuthorizesBeforeReading(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing organization", "missing session", "viewer", "API read only", "wrong API organization", "nil encrypter", "user encrypter", "wrong organization encrypter", "unavailable storage"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			org := &organizations.Organization{ID: 1, ExternalID: "organization"}
			keys := new(uploadKeys)
			ctx := encrypt.WithContext(uploadSessionContext(t.Context(), org, organizations.RoleMember), encrypt.ForOrganization(keys, org.ExternalID))
			store := new(uploadStore)
			m := &Mux{store: store}
			status, code := http.StatusForbidden, errors.ErrorCode(errors.ErrorCodeDOC02)
			switch name {
			case "missing organization":
				ctx = organizations.WithContext(ctx, nil)
				code = errors.ErrorCodeDOC01
			case "missing session":
				ctx = sessions.WithContext(ctx, 0)
			case "viewer":
				ctx = organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: organizations.RoleViewer})
			case "API read only":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeRead}})
			case "wrong API organization":
				ctx = apikeys.WithContext(ctx, &apikeys.Key{ID: 1, OrganizationID: 2, Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
			case "nil encrypter":
				ctx = encrypt.WithContext(ctx, nil)
			case "user encrypter":
				ctx = encrypt.WithContext(ctx, encrypt.ForUser(keys))
			case "wrong organization encrypter":
				ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(keys, "other-organization"))
			case "unavailable storage":
				m.store = nil
				status, code = http.StatusServiceUnavailable, errors.ErrorCodeDOC08
			}
			body := new(unreadUploadBody)
			req := httptest.NewRequest(http.MethodPost, "/documents", body).WithContext(ctx)
			req.Header.Set("Content-Type", "multipart/form-data; boundary=upload")
			rec := httptest.NewRecorder()
			m.Upload(rec, req)
			require.Equal(t, status, rec.Code)
			requireUploadError(t, rec, code)
			require.Zero(t, body.reads)
			require.Zero(t, store.puts)
			require.Zero(t, keys.orgCalls)
			require.Zero(t, keys.userCalls)
		})
	}
}

func TestUploadFilename(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		filename string
		want     string
		invalid  bool
	}{
		{name: "Windows path", filename: ` C:\scans\letter.pdf `, want: "letter.pdf"},
		{name: "POSIX path", filename: " /scans/letter.pdf ", want: "letter.pdf"},
		{name: "Unicode boundary", filename: strings.Repeat("文", 255), want: strings.Repeat("文", 255)},
		{name: "too long", filename: strings.Repeat("文", 256), invalid: true},
		{name: "invalid UTF8", filename: "letter\xff.pdf", invalid: true},
		{name: "control character", filename: "letter\u0085.pdf", invalid: true},
		{name: "empty", invalid: true},
		{name: "parent path", filename: "..", invalid: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			form := uploadForm{Filename: tt.filename}
			form.Normalize()
			err := form.Validate()
			if tt.invalid {
				require.ErrorIs(t, err, ErrInvalidFilename)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, form.Filename)
		})
	}
}

func TestUploadFailuresKeepPendingHidden(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create", "KMS", "object write", "finalize", "finalize missing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Documents")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			keys := new(uploadKeys)
			ctx := encrypt.WithContext(uploadSessionContext(t.Context(), org, organizations.RoleMember), encrypt.ForOrganization(keys, org.ExternalID))
			store := new(uploadStore)
			m := &Mux{db: db, store: store}
			switch name {
			case "create":
				m.db = uploadFailDB{Database: db, operation: "INSERT INTO documents", err: errors.New("database unavailable")}
			case "KMS":
				keys.err = errors.New("KMS unavailable")
			case "object write":
				store.err = errors.New("object write outcome unknown")
			case "finalize":
				m.db = uploadFailDB{Database: db, operation: "UPDATE documents", err: errors.New("database unavailable")}
			case "finalize missing":
				m.db = uploadFailDB{Database: db, operation: "UPDATE documents", err: sql.ErrNoRows}
			}
			rec := httptest.NewRecorder()
			m.Upload(rec, uploadRequest(t, "letter.txt", []byte("synthetic private correspondence")).WithContext(ctx))
			require.Equal(t, http.StatusInternalServerError, rec.Code)
			require.NotContains(t, rec.Body.String(), "unavailable")
			require.NotContains(t, rec.Body.String(), "synthetic private")
			require.Zero(t, store.deletes)
			var count int
			require.NoError(t, db.QueryRow(ctx, "SELECT count(*) FROM documents WHERE organization_id = $1", org.ID).Scan(&count))
			if name == "create" {
				require.Zero(t, count)
				require.Zero(t, store.puts)
				require.Zero(t, keys.orgCalls)
				return
			}
			require.Equal(t, 1, count)
			var externalID, status string
			require.NoError(t, db.QueryRow(ctx, "SELECT external_id, status FROM documents WHERE organization_id = $1", org.ID).Scan(&externalID, &status))
			require.Equal(t, "pending", status)
			got, err := documents.GetByExternalID(ctx, db, org.ID, externalID)
			require.NoError(t, err)
			require.Nil(t, got)
			page, err := documents.List(ctx, db, org.ID, pagination.Params{})
			require.NoError(t, err)
			require.Empty(t, page.Data)
			if name == "KMS" {
				require.Zero(t, store.puts)
			} else {
				require.Equal(t, 1, store.puts)
				require.NotEmpty(t, store.data)
			}
		})
	}
}

func uploadSessionContext(ctx context.Context, org *organizations.Organization, role organizations.Role) context.Context {
	ctx = organizations.WithContext(ctx, org)
	ctx = users.WithContext(ctx, &users.User{ID: 1})
	ctx = sessions.WithContext(ctx, 1)
	return organizations.WithMemberContext(ctx, &organizations.Member{ID: 1, UserID: 1, OrganizationID: org.ID, Role: role})
}

func uploadRequest(t *testing.T, filename string, data []byte) *http.Request {
	t.Helper()
	return multipartRequest(t, func(w *multipart.Writer) {
		part, err := w.CreateFormFile("file", filename)
		require.NoError(t, err)
		_, err = part.Write(data)
		require.NoError(t, err)
	})
}

func multipartRequest(t *testing.T, fill func(*multipart.Writer)) *http.Request {
	t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	fill(w)
	require.NoError(t, w.Close())
	req := httptest.NewRequest(http.MethodPost, "/documents", &body)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

func requireUploadError(t *testing.T, rec *httptest.ResponseRecorder, code errors.ErrorCode) {
	t.Helper()
	var response struct {
		Errors []errors.ErrorDetail `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	require.Len(t, response.Errors, 1)
	require.Equal(t, code, response.Errors[0].Code)
}

type uploadKeys struct {
	err       error
	orgCalls  int
	userCalls int
	returned  []byte
}

func (k *uploadKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	k.orgCalls++
	k.returned = bytes.Repeat([]byte{7}, 32)
	return k.returned, k.err
}

func (k *uploadKeys) UserKey(context.Context) ([]byte, error) {
	k.userCalls++
	return nil, errors.New("upload must not resolve user keys")
}

type uploadStore struct {
	puts      int
	deletes   int
	key       string
	data      []byte
	opts      *objectstore.PutOptions
	err       error
	beforePut func(string)
}

func (s *uploadStore) Put(ctx context.Context, key string, data io.Reader, opts *objectstore.PutOptions) error {
	s.puts++
	if s.beforePut != nil {
		s.beforePut(key)
	}
	s.key, s.opts = key, opts
	var err error
	s.data, err = io.ReadAll(data)
	if err != nil {
		return errors.Wrap(err, "unable to capture upload")
	}
	return s.err
}

func (s *uploadStore) Get(context.Context, string, *objectstore.GetOptions) (*objectstore.Object, error) {
	return nil, objectstore.ErrNotFound
}
func (s *uploadStore) Delete(context.Context, string) error { s.deletes++; return nil }
func (s *uploadStore) Head(context.Context, string) (*objectstore.ObjectInfo, error) {
	return nil, objectstore.ErrNotFound
}
func (s *uploadStore) List(context.Context, string) ([]objectstore.ObjectInfo, error) {
	return nil, nil
}
func (s *uploadStore) Copy(context.Context, string, string) error {
	return errors.New("unexpected copy")
}

type unreadUploadBody struct{ reads int }

func (b *unreadUploadBody) Read([]byte) (int, error) { b.reads++; return 0, io.ErrUnexpectedEOF }

type uploadFailDB struct {
	database.Database
	operation string
	err       error
}

func (db uploadFailDB) QueryRow(ctx context.Context, query string, args ...any) database.Row {
	if strings.Contains(query, db.operation) {
		return uploadErrorRow{err: db.err}
	}
	return db.Database.QueryRow(ctx, query, args...)
}

type uploadErrorRow struct{ err error }

func (r uploadErrorRow) Scan(...any) error { return r.err }
