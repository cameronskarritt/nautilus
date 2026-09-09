package mcp

import (
	"bytes"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/config"
	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/oauth"
	"nautilus/internal/enums"
	"nautilus/internal/log"
	"nautilus/internal/testutil/require"
)

func TestDownloadDocument(t *testing.T) {
	// Each subtest configures the issuer before constructing its server and actor.
	for _, mode := range []string{"API key", "canonical PDF", "OAuth"} {
		t.Run(mode, func(t *testing.T) {
			t.Cleanup(func() { config.SetProvider(new(config.EnvProvider)) })
			server := httptest.NewUnstartedServer(nil)
			t.Cleanup(server.Close)
			t.Setenv("MCP_BASE_URL", "http://"+server.Listener.Addr().String())
			config.SetProvider(new(config.EnvProvider))
			f := newDocumentActor(t, mode == "OAuth", enums.ScopeRead)
			plaintext := []byte("%PDF-1.7\nprivate document bytes\n")
			opts := &documents.CreateOptions{Filename: "private report.pdf", ContentType: "application/pdf", Size: int64(len(plaintext))}
			if mode == "canonical PDF" {
				opts.Pages = []documents.PageOptions{{ContentType: "image/png", Size: 10}}
			}
			doc, err := documents.Create(t.Context(), f.db, f.org.ID, opts)
			require.NoError(t, err)
			objectKey := doc.ObjectKey
			if mode == "canonical PDF" {
				objectKey += "/pdf/" + strings.Repeat("a", 64)
				doc, err = documents.PublishPDF(t.Context(), f.db, f.org.ID, doc.ExternalID, objectKey, int64(len(plaintext)))
			} else {
				doc, err = documents.MarkUploaded(t.Context(), f.db, f.org.ID, doc.ExternalID)
			}
			require.NoError(t, err)
			ciphertext, err := encrypt.ForOrganization(new(documentKeys), f.org.ExternalID).Seal(t.Context(), plaintext, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
			require.NoError(t, err)
			f.store.data[objectKey] = ciphertext
			var logs downloadLogs
			logger := log.New(slog.NewJSONHandler(&logs, nil))
			handler := NewHandler(f.db, f.store, f.keys, logger)
			server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				handler.ServeHTTP(w, r.WithContext(log.WithContext(r.Context(), logger)))
			})
			server.Start()
			session, err := mcp.NewClient(&mcp.Implementation{Name: "download test"}, nil).Connect(t.Context(), &mcp.StreamableClientTransport{
				Endpoint: server.URL + "/mcp", HTTPClient: &http.Client{Transport: documentTransport{header: f.header, token: f.token, base: server.Client().Transport}},
			}, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })
			var download downloadOutput
			decodeDocumentResult(t, callDocumentTool(t, session, "download_document", map[string]any{"document_id": doc.ExternalID}), &download)
			require.Equal(t, doc.ExternalID, download.DocumentID)
			require.Equal(t, doc.Filename, download.Filename)
			require.Equal(t, doc.ContentType, download.ContentType)
			require.Equal(t, int64(len(plaintext)), download.Size)
			require.WithinDuration(t, time.Now().Add(5*time.Minute), download.ExpiresAt, 5*time.Second)
			target, err := url.Parse(download.DownloadURL)
			require.NoError(t, err)
			require.Equal(t, server.URL, target.Scheme+"://"+target.Host)
			require.Equal(t, "/mcp/download", target.Path)
			token := target.Query().Get("token")
			require.NotEmpty(t, token)
			require.NotContains(t, download.DownloadURL, f.token)
			require.Zero(t, f.store.gets.Load())
			require.Zero(t, f.keys.calls.Load())
			for _, tt := range []struct {
				method, byteRange string
				status            int
				body              []byte
			}{{http.MethodGet, "", http.StatusOK, plaintext}, {http.MethodHead, "", http.StatusOK, nil}, {http.MethodGet, "bytes=0-3", http.StatusPartialContent, plaintext[:4]}} {
				req, err := http.NewRequestWithContext(t.Context(), tt.method, download.DownloadURL, nil)
				require.NoError(t, err)
				req.Header.Set("Range", tt.byteRange)
				response, err := http.DefaultClient.Do(req)
				require.NoError(t, err)
				data, err := io.ReadAll(response.Body)
				response.Body.Close()
				require.NoError(t, err)
				require.Equal(t, tt.status, response.StatusCode)
				require.Equal(t, string(tt.body), string(data))
				require.Equal(t, "application/pdf", response.Header.Get("Content-Type"))
				require.Equal(t, "no-store", response.Header.Get("Cache-Control"))
				require.Equal(t, "no-referrer", response.Header.Get("Referrer-Policy"))
				require.Equal(t, "nosniff", response.Header.Get("X-Content-Type-Options"))
				disposition, params, err := mime.ParseMediaType(response.Header.Get("Content-Disposition"))
				require.NoError(t, err)
				require.Equal(t, "attachment", disposition)
				require.Equal(t, doc.Filename, params["filename"])
				if tt.byteRange == "" {
					require.Equal(t, strconv.Itoa(len(plaintext)), response.Header.Get("Content-Length"))
				}
			}
			rangeRequest, err := http.NewRequestWithContext(t.Context(), http.MethodGet, download.DownloadURL, nil)
			require.NoError(t, err)
			rangeRequest.Header.Set("Range", "bytes=10000-")
			rangeResponse, err := http.DefaultClient.Do(rangeRequest)
			require.NoError(t, err)
			rangeResponse.Body.Close()
			require.Equal(t, http.StatusRequestedRangeNotSatisfiable, rangeResponse.StatusCode)
			require.Equal(t, "no-store", rangeResponse.Header.Get("Cache-Control"))
			for _, path := range []string{"/mcp//download", "/mcp/unused/../download"} {
				response, err := http.Get(server.URL + path + "?token=" + token)
				require.NoError(t, err)
				response.Body.Close()
				require.Equal(t, http.StatusOK, response.StatusCode)
			}
			reads := f.store.gets.Load()
			for _, query := range []string{"", "token=invalid", "token=" + token + "x", "token=" + token + "&token=" + token, "token=" + token + "&extra=yes", "token=" + token + "&broken=%"} {
				response, err := http.Get(server.URL + "/mcp/download?" + query)
				require.NoError(t, err)
				response.Body.Close()
				require.Equal(t, http.StatusNotFound, response.StatusCode)
			}
			require.Equal(t, reads, f.store.gets.Load())
			require.NotContains(t, logs.String(), token)
			require.NotContains(t, logs.String(), f.token)
			// A second handler instance resolves the same ticket from the shared DB.
			other := httptest.NewServer(NewHandler(f.db, f.store, f.keys, log.New(slog.DiscardHandler)))
			t.Cleanup(other.Close)
			response, err := http.Get(other.URL + target.RequestURI())
			require.NoError(t, err)
			data, err := io.ReadAll(response.Body)
			response.Body.Close()
			require.NoError(t, err)
			require.Equal(t, http.StatusOK, response.StatusCode)
			require.Equal(t, plaintext, data)
			if mode == "OAuth" {
				access := strings.TrimPrefix(f.token, "Bearer ")
				grant, err := oauth.Authenticate(t.Context(), f.db, access, server.URL+"/mcp")
				require.NoError(t, err)
				require.NoError(t, oauth.Revoke(t.Context(), f.db, grant.ClientID, access))
			} else {
				key, err := apikeys.Authenticate(t.Context(), f.db, f.token)
				require.NoError(t, err)
				revoked, err := apikeys.RevokeByExternalID(t.Context(), f.db, f.org.ID, key.ExternalID)
				require.NoError(t, err)
				require.True(t, revoked)
			}
			reads = f.store.gets.Load()
			response, err = http.Get(download.DownloadURL)
			require.NoError(t, err)
			response.Body.Close()
			require.Equal(t, http.StatusNotFound, response.StatusCode)
			require.Equal(t, reads, f.store.gets.Load())
			require.Equal(t, reads, f.store.closed.Load())
			require.NotContains(t, logs.String(), token)
		})
	}
}

type downloadLogs struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (l *downloadLogs) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n, _ := l.buffer.Write(data)
	return n, nil
}

func (l *downloadLogs) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buffer.String()
}
