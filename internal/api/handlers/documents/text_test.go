package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"uuid"

	"nautilus/internal/api/authentication"
	"nautilus/internal/api/version"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/mux"
	"nautilus/internal/mux/middleware"
	"nautilus/internal/objectstore"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentText(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		status int
		code   string
	}{
		{"UTF-8", 200, ""},
		{"empty", 200, ""},
		{"uploading", 409, "DOC-12"},
		{"failed", 409, "DOC-12"},
		{"OCR pending", 409, "DOC-12"},
		{"missing document", 404, "HTTP-404"},
		{"other tenant", 404, "HTTP-404"},
		{"missing bearer", 401, "APIKEY-09"},
		{"invalid bearer", 401, "APIKEY-09"},
		{"insufficient scope", 403, "APIKEY-10"},
		{"invalid UUID", 404, ""},
		{"unsupported version", 400, "API-01"},
		{"unsupported method", 405, ""},
		{"wrong document binding", 500, "HTTP-500"},
		{"wrong purpose", 500, "HTTP-500"},
		{"wrong organization binding", 500, "HTTP-500"},
		{"tampered", 500, "HTTP-500"},
		{"invalid UTF-8", 500, "HTTP-500"},
		{"store failure", 500, "HTTP-500"},
		{"read failure", 500, "HTTP-500"},
		{"oversized object", 500, "HTTP-500"},
		{"storage unavailable", 503, "DOC-13"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := testutil.SetupTestDB(t)
			userID := testutil.CreateTestUser(t, db, nil)
			orgID := testutil.CreateTestOrg(t, db, "text", "Text")
			org, err := organizations.Get(t.Context(), db, orgID)
			require.NoError(t, err)
			doc, err := documents.Create(t.Context(), db, orgID, &documents.CreateOptions{Filename: "letter.pdf", ContentType: "application/pdf", Size: 4})
			require.NoError(t, err)
			state := enums.DocumentStatusUploaded
			if tt.name == "uploading" {
				state = enums.DocumentStatusUploading
			}
			if tt.name == "failed" {
				state = enums.DocumentStatusFailed
			}
			_, err = db.Exec(t.Context(), "UPDATE documents SET status = $1 WHERE external_id = $2", state, doc.ExternalID)
			require.NoError(t, err)
			keyOrg := orgID
			if tt.name == "other tenant" {
				keyOrg = testutil.CreateTestOrg(t, db, "other", "Other")
			}
			scope := enums.ScopeRead
			if tt.name == "insufficient scope" {
				scope = enums.ScopeWrite
			}
			_, token, err := apikeys.Create(t.Context(), db, keyOrg, userID, &apikeys.CreateOptions{Name: "text", Scopes: []enums.Scope{scope}})
			require.NoError(t, err)
			data := []byte("Bonjour été — 日本語\n")
			if tt.name == "empty" {
				data = nil
			}
			if tt.name == "invalid UTF-8" {
				data = []byte{0xff}
			}
			binding := encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID}
			if tt.name == "wrong document binding" {
				binding.RecordID = uuid.New().String()
			}
			if tt.name == "wrong purpose" {
				binding.Purpose = "document"
			}
			organizationID := org.ExternalID
			if tt.name == "wrong organization binding" {
				organizationID = uuid.New().String()
			}
			sealed, err := encrypt.ForOrganization(textKeys{}, organizationID).Seal(t.Context(), data, binding)
			require.NoError(t, err)
			if tt.name == "tampered" {
				sealed[len(sealed)-1] ^= 1
			}
			store := &textStore{data: sealed}
			if tt.name == "oversized object" {
				store.remaining = encrypt.MaxPlaintextSize + 128<<10
			}
			if tt.name == "OCR pending" {
				store.err = objectstore.ErrNotFound
			}
			if tt.name == "store failure" {
				store.err = errors.New("synthetic storage failure")
			}
			if tt.name == "read failure" {
				store.readErr = true
			}
			router := mux.New(mux.Config{Middleware: []mux.Middleware{authentication.RequireAPIKey(db), middleware.OrganizationEncryption(textKeys{}), version.Middleware}})
			var storage objectstore.Store = store
			if tt.name == "storage unavailable" {
				storage = nil
			}
			Mount(router, db, storage, nil)
			id := doc.ExternalID
			if tt.name == "missing document" {
				id = uuid.New().String()
			}
			if tt.name == "invalid UUID" {
				id = "invalid"
			}
			method := http.MethodGet
			if tt.name == "unsupported method" {
				method = http.MethodPost
			}
			req := httptest.NewRequest(method, "/documents/"+id+"/text", nil)
			if tt.name == "invalid bearer" {
				token = "invalid"
			}
			if tt.name != "missing bearer" {
				req.Header.Set("Authorization", "Bearer "+token)
			}
			req.Header.Set("X-API-Version", "2026-01-01")
			if tt.name == "unsupported version" {
				req.Header.Set("X-API-Version", "2099-01-01")
			}
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			require.Equal(t, tt.status, rec.Code)
			if tt.name == "unsupported method" {
				require.Contains(t, rec.Header().Get("Allow"), http.MethodGet)
				require.NotContains(t, rec.Header().Get("Allow"), http.MethodPost)
			}
			if tt.code != "" {
				var result struct {
					Errors []errors.ErrorDetail `json:"errors"`
				}
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &result))
				require.Len(t, result.Errors, 1)
				require.Equal(t, errors.ErrorCode(tt.code), result.Errors[0].Code)
				require.Empty(t, result.Errors[0].Field)
				require.NotContains(t, rec.Body.String(), "synthetic storage failure")
			} else if tt.status == http.StatusOK {
				require.Equal(t, string(data), rec.Body.String())
				require.Equal(t, "text/plain; charset=utf-8", rec.Header().Get("Content-Type"))
				require.Equal(t, strconv.Itoa(len(data)), rec.Header().Get("Content-Length"))
				require.Equal(t, "nosniff", rec.Header().Get("X-Content-Type-Options"))
				require.Equal(t, "no-store", rec.Header().Get("Cache-Control"))
			}
			if store.gets > 0 {
				require.Equal(t, doc.ObjectKey+"/ocr", store.key)
			}
			if store.gets > 0 && store.err == nil {
				require.True(t, store.closed)
			}
			if tt.name == "oversized object" {
				require.Equal(t, 64<<10-1, store.remaining)
			}
			if tt.status < 500 && tt.name != "OCR pending" && tt.status != 200 {
				require.Zero(t, store.gets)
			}
		})
	}
}

type textKeys struct{}

func (textKeys) OrganizationKey(context.Context, string) ([]byte, error) {
	return bytes.Repeat([]byte{7}, 32), nil
}

func (textKeys) UserKey(context.Context) ([]byte, error) {
	return nil, errors.New("unexpected user key")
}

type textStore struct {
	objectstore.Store
	data      []byte
	err       error
	readErr   bool
	gets      int
	key       string
	closed    bool
	remaining int
}

func (s *textStore) Get(_ context.Context, key string, _ *objectstore.GetOptions) (*objectstore.Object, error) {
	s.gets++
	s.key = key
	if s.err != nil {
		return nil, s.err
	}
	return &objectstore.Object{Body: s}, nil
}

func (s *textStore) Read(p []byte) (int, error) {
	if s.remaining > 0 {
		n := min(len(p), s.remaining)
		clear(p[:n])
		s.remaining -= n
		return n, nil
	}
	if s.readErr {
		return 0, io.ErrUnexpectedEOF
	}
	if len(s.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, s.data)
	s.data = s.data[n:]
	return n, nil
}

func (s *textStore) Close() error { s.closed = true; return nil }
