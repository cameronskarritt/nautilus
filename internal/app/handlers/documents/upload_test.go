package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/mocks"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/database/sessions"
	"nautilus/internal/database/users"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
	"nautilus/internal/workflows/upload"
)

func TestUploadEncryptedDocument(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name, filename, wantName string
		formats                  []string
		role                     organizations.Role
		api                      bool
	}{
		{name: "member", filename: " letter.png ", wantName: "letter.pdf", formats: []string{"png"}, role: organizations.RoleMember},
		{name: "owner", filename: "letter.jpg", wantName: "letter.pdf", formats: []string{"jpeg"}, role: organizations.RoleOwner},
		{name: "admin", filename: `C:\scans\letter.png`, wantName: "letter.pdf", formats: []string{"png"}, role: organizations.RoleAdmin},
		{name: "API write", filename: "/scans/letter.png", wantName: "letter.pdf", formats: []string{"png"}, api: true},
		{name: "ordered mixed pages", filename: "letter.png", wantName: "letter.pdf", formats: []string{"png", "jpeg", "png"}, role: organizations.RoleMember},
		{name: "extension is not trusted", filename: "letter.pdf", wantName: "letter.pdf", formats: []string{"png"}, role: organizations.RoleMember},
		{name: "long output filename", filename: strings.Repeat("文", 255), wantName: strings.Repeat("文", 251) + ".pdf", formats: []string{"png"}, role: organizations.RoleMember},
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
				ctx = apikeys.WithContext(organizations.WithContext(t.Context(), org), &apikeys.Key{ID: 1, OrganizationID: org.ID, Scopes: []apikeys.Scope{apikeys.ScopeWrite}})
			}
			ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(keys, org.ExternalID))
			store := &uploadStore{beforePut: func(key string) {
				var status string
				require.NoError(t, db.QueryRow(ctx, "SELECT status FROM documents WHERE object_key = $1", strings.Split(key, "/pages/")[0]).Scan(&status))
				require.Equal(t, "uploading", status)
			}}
			workflows := mocks.NewClient(t)
			workflows.On("ExecuteWorkflow", mock.Anything, mock.Anything, upload.Name, mock.Anything).Run(func(args mock.Arguments) {
				require.Equal(t, len(tt.formats), store.puts, "all source images must be stored before starting workflow")
				input := args.Get(3).(upload.Input)
				require.Equal(t, org.ID, input.OrganizationID)
				require.Equal(t, "documents/"+input.DocumentID+"/pages/"+strconv.Itoa(len(tt.formats)), store.key)
			}).Return(nil, nil).Once()
			var images [][]byte
			req := multipartRequest(t, func(w *multipart.Writer) {
				for _, format := range tt.formats {
					data := uploadImage(t, format)
					images = append(images, data)
					part, err := w.CreateFormFile("file", tt.filename)
					require.NoError(t, err)
					_, err = part.Write(data)
					require.NoError(t, err)
				}
			}).WithContext(ctx)
			rec := httptest.NewRecorder()
			(&Mux{db: db, store: store, workflows: workflows}).Upload(rec, req)
			require.Equal(t, http.StatusAccepted, rec.Code, rec.Body.String())
			require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			var response struct {
				Document documents.Document `json:"document"`
			}
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
			doc := response.Document
			require.NotEmpty(t, doc.ExternalID)
			require.Equal(t, tt.wantName, doc.Filename)
			require.Equal(t, "application/pdf", doc.ContentType)
			require.Zero(t, doc.Size, "PDF size is set by the worker")
			require.Equal(t, len(images), doc.PageCount)
			for _, field := range []string{"object_key", "organization_id", "pages"} {
				require.NotContains(t, rec.Body.String(), `"`+field+`"`)
			}
			require.Equal(t, len(images), store.puts)
			require.Zero(t, store.deletes)
			require.Equal(t, "application/octet-stream", store.opts.ContentType.Data)
			require.False(t, store.opts.Metadata.Set)
			pages, err := documents.ListPages(ctx, db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.Len(t, pages, len(images))
			for i, page := range pages {
				require.Equal(t, i+1, page.Number)
				require.Equal(t, "image/"+tt.formats[i], page.ContentType)
				require.Equal(t, int64(len(images[i])), page.Size)
				ciphertext := store.objects[page.ObjectKey]
				require.False(t, bytes.Contains(ciphertext, images[i]))
				binding := encrypt.Binding{Purpose: "document-page", RecordID: doc.ExternalID + "/" + strconv.Itoa(i+1)}
				plaintext, err := encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, ciphertext, binding)
				require.NoError(t, err)
				require.Equal(t, images[i], plaintext)
				clear(plaintext)
				for _, wrong := range []encrypt.Binding{
					{Purpose: "document", RecordID: doc.ExternalID},
					{Purpose: "document-page", RecordID: doc.ExternalID + "/999"},
					{Purpose: "document-page", RecordID: "another-document/" + strconv.Itoa(i+1)},
				} {
					_, err = encrypt.ForOrganization(keys, org.ExternalID).Open(ctx, ciphertext, wrong)
					require.ErrorIs(t, err, encrypt.ErrInvalidEnvelope)
				}
				_, err = encrypt.ForOrganization(keys, "another-organization").Open(ctx, ciphertext, binding)
				require.ErrorIs(t, err, encrypt.ErrInvalidEnvelope)
			}
			stored, err := documents.GetByExternalID(ctx, db, org.ID, doc.ExternalID)
			require.NoError(t, err)
			require.Equal(t, enums.DocumentStatusUploading, stored.Status)
			require.Equal(t, enums.DocumentStatusUploading, doc.Status)
			require.Zero(t, keys.userCalls)
			require.Equal(t, make([]byte, 32), keys.returned)
		})
	}
}

func uploadImage(t *testing.T, format string) []byte {
	t.Helper()
	var b bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 24))
	if format == "jpeg" {
		require.NoError(t, jpeg.Encode(&b, img, nil))
	} else {
		require.NoError(t, png.Encode(&b, img))
	}
	return b.Bytes()
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
		{name: "too many pages", build: func(t *testing.T) *http.Request {
			t.Helper()
			return multipartRequest(t, func(w *multipart.Writer) {
				for range 101 {
					_, err := w.CreateFormFile("file", "letter.txt")
					require.NoError(t, err)
				}
			})
		}, status: http.StatusBadRequest, code: errors.ErrorCodeDOC11},
		{name: "PDF rejected", build: func(t *testing.T) *http.Request {
			t.Helper()

			return uploadRequest(t, "scan.png", []byte("%PDF-1.7\nsynthetic private document"))
		}, status: http.StatusUnprocessableEntity, code: errors.ErrorCodeDOC10},
		{name: "text rejected", build: func(t *testing.T) *http.Request {
			t.Helper()

			return uploadRequest(t, "scan.png", []byte("synthetic private text"))
		}, status: http.StatusUnprocessableEntity, code: errors.ErrorCodeDOC10},
		{name: "empty rejected", build: func(t *testing.T) *http.Request {
			t.Helper()
			return uploadRequest(t, "scan.png", nil)
		}, status: http.StatusUnprocessableEntity, code: errors.ErrorCodeDOC10},
		{name: "corrupt image", build: func(t *testing.T) *http.Request {
			t.Helper()

			data := uploadImage(t, "png")
			return uploadRequest(t, "scan.png", data[:len(data)/2])
		}, status: http.StatusUnprocessableEntity, code: errors.ErrorCodeDOC10},
		{name: "invalid later page", build: func(t *testing.T) *http.Request {
			t.Helper()

			return multipartRequest(t, func(w *multipart.Writer) {
				for _, data := range [][]byte{uploadImage(t, "png"), []byte("synthetic private text")} {
					part, err := w.CreateFormFile("file", "scan.png")
					require.NoError(t, err)
					_, err = part.Write(data)
					require.NoError(t, err)
				}
			})
		}, status: http.StatusUnprocessableEntity, code: errors.ErrorCodeDOC10},

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
			(&Mux{store: store, workflows: mocks.NewClient(t)}).Upload(rec, tt.build(t).WithContext(ctx))
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
	for _, name := range []string{"missing organization", "missing session", "viewer", "API read only", "wrong API organization", "nil encrypter", "user encrypter", "wrong organization encrypter", "unavailable storage", "unavailable workflows"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			org := &organizations.Organization{ID: 1, ExternalID: "organization"}
			keys := new(uploadKeys)
			ctx := encrypt.WithContext(uploadSessionContext(t.Context(), org, organizations.RoleMember), encrypt.ForOrganization(keys, org.ExternalID))
			store := new(uploadStore)
			m := &Mux{store: store, workflows: mocks.NewClient(t)}
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
			case "unavailable workflows":
				m.workflows = nil
				status, code = http.StatusServiceUnavailable, errors.ErrorCodeDOC09
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

func TestUploadFailures(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"create", "KMS", "object write", "workflow start"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			orgID := testutil.CreateTestOrg(t, db, t.Name(), "Documents")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			keys := new(uploadKeys)
			ctx := encrypt.WithContext(uploadSessionContext(t.Context(), org, organizations.RoleMember), encrypt.ForOrganization(keys, org.ExternalID))
			store := new(uploadStore)
			workflows := mocks.NewClient(t)
			m := &Mux{db: db, store: store, workflows: workflows}
			switch name {
			case "create":
				m.db = uploadFailDB{Database: db, err: errors.New("database unavailable")}
			case "KMS":
				keys.err = errors.New("KMS unavailable")
			case "object write":
				store.err = errors.New("object write outcome unknown")
			case "workflow start":
				workflows.On("ExecuteWorkflow", mock.Anything, mock.Anything, upload.Name, mock.Anything).Return(nil, context.DeadlineExceeded).Once()
			}
			rec := httptest.NewRecorder()
			m.Upload(rec, uploadRequest(t, "letter.png", uploadImage(t, "png")).WithContext(ctx))
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
			page, err := documents.List(ctx, db, org.ID, pagination.Params{})
			require.NoError(t, err)
			require.Len(t, page.Data, 1)
			if name == "workflow start" {
				require.Equal(t, enums.DocumentStatusUploading, page.Data[0].Status)
				completed, err := documents.PublishPDF(ctx, db, org.ID, page.Data[0].ExternalID, page.Data[0].ObjectKey+"/pdf/"+strings.Repeat("a", 64), 123)
				require.NoError(t, err)
				require.NotNil(t, completed, "an accepted workflow must still be able to finalize after a start timeout")
				require.Equal(t, enums.DocumentStatusUploaded, completed.Status)
			} else {
				require.Equal(t, enums.DocumentStatusFailed, page.Data[0].Status)
			}
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
	objects   map[string][]byte
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
	if s.objects == nil {
		s.objects = make(map[string][]byte)
	}
	s.objects[key] = s.data
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
	err error
}

func (db uploadFailDB) Begin(context.Context) (database.Transaction, error) { return nil, db.err }
