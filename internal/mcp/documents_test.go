package mcp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/config"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/pagination"
	"nautilus/internal/testutil"
	"nautilus/internal/testutil/require"
)

func TestDocumentToolsRead(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"API key", "OAuth"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := newDocumentActor(t, method == "OAuth", enums.ScopeRead)
			first := f.document(t, f.org.ID, "first.pdf", true)
			second := f.document(t, f.org.ID, "second.pdf", true)
			foreignOrg := testutil.CreateTestOrg(t, f.db, "foreign", "Foreign")
			foreign := f.document(t, foreignOrg, "foreign.pdf", true)
			f.text(t, second, "Aé🙂Z", "document-ocr", second.ExternalID)
			session := f.connect(t)
			var page pagination.Page[*documents.Document]
			decodeDocumentResult(t, callDocumentTool(t, session, "list_documents", map[string]any{"limit": 1}), &page)
			require.Len(t, page.Data, 1)
			require.Equal(t, second.ExternalID, page.Data[0].ExternalID)
			require.True(t, page.HasMore)
			require.NotEmpty(t, page.NextCursor)
			decodeDocumentResult(t, callDocumentTool(t, session, "list_documents", map[string]any{"cursor": page.NextCursor, "limit": 1}), &page)
			require.Len(t, page.Data, 1)
			require.Equal(t, first.ExternalID, page.Data[0].ExternalID)
			require.False(t, page.HasMore)
			var got struct{ Document *documents.Document }
			result := callDocumentTool(t, session, "get_document", map[string]any{"document_id": second.ExternalID})
			decodeDocumentResult(t, result, &got)
			require.Equal(t, second.ExternalID, got.Document.ExternalID)
			require.Equal(t, "second.pdf", got.Document.Filename)
			wire, err := json.Marshal(result)
			require.NoError(t, err)
			for _, hidden := range []string{"object_key", "pdf_key", "organization_id", second.ObjectKey} {
				require.NotContains(t, string(wire), hidden)
			}
			for _, tool := range []string{"get_document", "read_document"} {
				require.True(t, callDocumentTool(t, session, tool, map[string]any{"document_id": foreign.ExternalID}).IsError)
			}
			for _, cursor := range []string{"invalid", base64.RawURLEncoding.EncodeToString([]byte("null")), pagination.Encode(pagination.Cursor{"id": strconv.Itoa(foreign.ID), "organization_id": strconv.Itoa(foreignOrg)})} {
				require.True(t, callDocumentTool(t, session, "list_documents", map[string]any{"cursor": cursor}).IsError)
			}
			require.Zero(t, f.store.gets.Load())
			require.Zero(t, f.keys.calls.Load())

			var text documentTextResult
			var combined string
			for _, expected := range []struct {
				offset int
				text   string
				next   int
			}{{0, "Aé", 3}, {3, "🙂", 7}, {7, "Z", 0}} {
				decodeDocumentResult(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": second.ExternalID, "offset": expected.offset, "limit": 4}), &text)
				require.Equal(t, second.ExternalID, text.DocumentID)
				require.Equal(t, expected.text, text.Text)
				require.Equal(t, expected.offset, text.Offset)
				require.Equal(t, len("Aé🙂Z"), text.TotalBytes)
				require.Equal(t, expected.next > 0, text.HasMore)
				if text.HasMore {
					require.Equal(t, expected.next, text.NextOffset)
				}
				combined += text.Text
			}
			require.Equal(t, "Aé🙂Z", combined)
			require.Equal(t, f.store.gets.Load(), f.store.closed.Load())
			for _, offset := range []int{-1, 2, 9} {
				require.True(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": second.ExternalID, "offset": offset}).IsError)
			}
		})
	}
}

func TestDocumentToolsRequireReadScope(t *testing.T) {
	t.Parallel()
	for _, method := range []string{"API key", "OAuth"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			f := newDocumentActor(t, method == "OAuth", enums.ScopeWrite)
			doc := f.document(t, f.org.ID, "private.pdf", true)
			session := f.connect(t)
			for _, tool := range []string{"list_documents", "get_document", "read_document"} {
				args := map[string]any{}
				if tool != "list_documents" {
					args["document_id"] = doc.ExternalID
				}
				require.True(t, callDocumentTool(t, session, tool, args).IsError)
			}
			require.Zero(t, f.store.gets.Load())
			require.Zero(t, f.keys.calls.Load())
		})
	}
}

func TestReadDocumentFailures(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"pending", "missing text", "empty text", "wrong record", "wrong purpose", "storage error", "key error"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := newDocumentActor(t, false, enums.ScopeRead)
			doc := f.document(t, f.org.ID, "private.pdf", mode != "pending")
			switch mode {
			case "empty text":
				f.text(t, doc, "", "document-ocr", doc.ExternalID)
			case "wrong record":
				f.text(t, doc, "secret plaintext", "document-ocr", "another-record")
			case "wrong purpose":
				f.text(t, doc, "secret plaintext", "document", doc.ExternalID)
			case "storage error":
				f.store.err = errors.New("secret storage diagnostic")
			case "key error":
				f.text(t, doc, "secret plaintext", "document-ocr", doc.ExternalID)
				f.keys.err = errors.New("secret key diagnostic")
			}
			result := callDocumentTool(t, f.connect(t), "read_document", map[string]any{"document_id": doc.ExternalID})
			if mode == "empty text" {
				var text documentTextResult
				decodeDocumentResult(t, result, &text)
				require.Empty(t, text.Text)
				require.Zero(t, text.TotalBytes)
				require.False(t, text.HasMore)
			} else {
				require.True(t, result.IsError)
				wire, err := json.Marshal(result)
				require.NoError(t, err)
				require.NotContains(t, string(wire), "secret")
				require.NotContains(t, string(wire), doc.ObjectKey)
			}
			if mode == "pending" {
				require.Zero(t, f.store.gets.Load())
				require.Zero(t, f.keys.calls.Load())
			}
		})
	}
}

func TestReadDocumentBounds(t *testing.T) {
	t.Parallel()
	f := newDocumentActor(t, false, enums.ScopeRead)
	doc := f.document(t, f.org.ID, "large.pdf", true)
	f.text(t, doc, strings.Repeat("a", 80000), "document-ocr", doc.ExternalID)
	session := f.connect(t)
	var text documentTextResult
	decodeDocumentResult(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": doc.ExternalID}), &text)
	require.Len(t, text.Text, 16384)
	require.Equal(t, 80000, text.TotalBytes)
	require.Equal(t, 16384, text.NextOffset)
	require.True(t, text.HasMore)
	decodeDocumentResult(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": doc.ExternalID, "limit": 65536}), &text)
	require.Len(t, text.Text, 65536)
	require.Equal(t, 65536, text.NextOffset)
	require.True(t, text.HasMore)
	decodeDocumentResult(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": doc.ExternalID, "offset": 80000}), &text)
	require.Empty(t, text.Text)
	require.False(t, text.HasMore)
	for _, limit := range []int{3, 65537} {
		require.True(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": doc.ExternalID, "limit": limit}).IsError)
	}
}

func TestDocumentMetadataWithoutStorage(t *testing.T) {
	t.Parallel()
	f := newDocumentActor(t, false, enums.ScopeRead)
	doc := f.document(t, f.org.ID, "metadata.pdf", true)
	server := httptest.NewServer(NewHandler(f.db, nil, nil, log.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "metadata test"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: documentTransport{header: f.header, token: f.token, base: server.Client().Transport}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	var page pagination.Page[*documents.Document]
	decodeDocumentResult(t, callDocumentTool(t, session, "list_documents", map[string]any{}), &page)
	require.Len(t, page.Data, 1)
	require.Equal(t, doc.ExternalID, page.Data[0].ExternalID)
	var got struct{ Document *documents.Document }
	decodeDocumentResult(t, callDocumentTool(t, session, "get_document", map[string]any{"document_id": doc.ExternalID}), &got)
	require.Equal(t, doc.ExternalID, got.Document.ExternalID)
	require.True(t, callDocumentTool(t, session, "read_document", map[string]any{"document_id": doc.ExternalID}).IsError)
}

type documentActor struct {
	db     database.Database
	org    *organizations.Organization
	header string
	token  string
	store  *documentStore
	keys   *documentKeys
}

func newDocumentActor(t *testing.T, useOAuth bool, scope enums.Scope) *documentActor {
	t.Helper()
	db := testutil.SetupTestDB(t)
	userID := testutil.CreateTestUser(t, db, nil)
	orgID := testutil.CreateTestOrg(t, db, "mcp-documents", "MCP Documents")
	org, err := organizations.Get(t.Context(), db, orgID)
	require.NoError(t, err)
	f := &documentActor{db: db, org: org, header: "X-API-Key", store: &documentStore{data: map[string][]byte{}}, keys: new(documentKeys)}
	if !useOAuth {
		_, f.token, err = apikeys.Create(t.Context(), db, orgID, userID, &apikeys.CreateOptions{Name: "MCP", Scopes: []enums.Scope{scope}})
		require.NoError(t, err)
		return f
	}
	memberID := testutil.CreateTestOrgMember(t, db, userID, orgID, enums.RoleMember)
	client, err := oauth.RegisterClient(t.Context(), db, "MCP", []string{"http://localhost/callback"})
	require.NoError(t, err)
	verifier := strings.Repeat("a", 43)
	challenge := sha256.Sum256([]byte(verifier))
	resource := strings.TrimRight(config.Get("MCP_BASE_URL", "http://localhost:8082"), "/") + "/mcp"
	code, err := oauth.CreateGrant(t.Context(), db, userID, memberID, &oauth.GrantOptions{
		ClientID: client.ID, RedirectURI: client.RedirectURIs[0], Resource: resource,
		Scope: string(scope), Challenge: base64.RawURLEncoding.EncodeToString(challenge[:]),
	})
	require.NoError(t, err)
	tokens, err := oauth.ExchangeCode(t.Context(), db, client.ID, code, client.RedirectURIs[0], resource, verifier)
	require.NoError(t, err)
	f.header = "Authorization"
	f.token = "Bearer " + tokens.AccessToken
	return f
}

func (f *documentActor) document(t *testing.T, orgID int, name string, uploaded bool) *documents.Document {
	t.Helper()
	doc, err := documents.Create(t.Context(), f.db, orgID, &documents.CreateOptions{Filename: name, ContentType: "application/pdf", Size: 10})
	require.NoError(t, err)
	if uploaded {
		doc, err = documents.MarkUploaded(t.Context(), f.db, orgID, doc.ExternalID)
		require.NoError(t, err)
	}
	return doc
}

func (f *documentActor) text(t *testing.T, doc *documents.Document, text, purpose, record string) {
	t.Helper()
	data, err := encrypt.ForOrganization(new(documentKeys), f.org.ExternalID).Seal(t.Context(), []byte(text), encrypt.Binding{Purpose: purpose, RecordID: record})
	require.NoError(t, err)
	f.store.data[doc.ObjectKey+"/ocr"] = data
}

func (f *documentActor) connect(t *testing.T) *mcp.ClientSession {
	t.Helper()
	server := httptest.NewServer(NewHandler(f.db, f.store, f.keys, log.New(slog.DiscardHandler)))
	t.Cleanup(server.Close)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "document test"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
		Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: documentTransport{header: f.header, token: f.token, base: server.Client().Transport}},
	}, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func callDocumentTool(t *testing.T, session *mcp.ClientSession, name string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	result, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: name, Arguments: args})
	require.NoError(t, err)
	return result
}

func decodeDocumentResult(t *testing.T, result *mcp.CallToolResult, value any) {
	t.Helper()
	require.False(t, result.IsError)
	data, err := json.Marshal(result.StructuredContent)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, value))
}

type documentTextResult struct {
	DocumentID string `json:"document_id"`
	Text       string `json:"text"`
	Offset     int    `json:"offset"`
	TotalBytes int    `json:"total_bytes"`
	NextOffset int    `json:"next_offset"`
	HasMore    bool   `json:"has_more"`
}

type documentTransport struct {
	header, token string
	base          http.RoundTripper
}

func (transport documentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set(transport.header, transport.token)
	response, err := transport.base.RoundTrip(req)
	return response, errors.Wrap(err, "document test request failed")
}

type documentKeys struct {
	calls atomic.Int64
	err   error
}

func (k *documentKeys) OrganizationKey(_ context.Context, id string) ([]byte, error) {
	k.calls.Add(1)
	key := sha256.Sum256([]byte(id))
	return key[:], k.err
}
func (*documentKeys) UserKey(context.Context) ([]byte, error) {
	return nil, errors.New("unexpected user encryption")
}

type documentStore struct {
	objectstore.Store
	data   map[string][]byte
	err    error
	gets   atomic.Int64
	closed atomic.Int64
}

func (s *documentStore) Get(_ context.Context, key string, _ *objectstore.GetOptions) (*objectstore.Object, error) {
	s.gets.Add(1)
	if s.err != nil {
		return nil, s.err
	}
	data, ok := s.data[key]
	if !ok {
		return nil, objectstore.ErrNotFound
	}
	return &objectstore.Object{Body: &documentBody{Reader: bytes.NewReader(data), closed: &s.closed}}, nil
}

type documentBody struct {
	io.Reader
	closed *atomic.Int64
}

func (b *documentBody) Close() error {
	b.closed.Add(1)
	return nil
}
