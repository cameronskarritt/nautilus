package mcp

import (
	"bytes"
	"context"
	"mime"
	"net/http"
	"net/url"
	"path"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documentdownloads"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/oauth"
	"nautilus/internal/database/organizations"
	"nautilus/internal/documentfiles"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
)

type downloadOutput struct {
	DocumentID  string    `json:"document_id"`
	DownloadURL string    `json:"download_url"`
	ExpiresAt   time.Time `json:"expires_at"`
	Filename    string    `json:"filename"`
	ContentType string    `json:"content_type"`
	Size        int64     `json:"size"`
}

func (d *documentTools) download(ctx context.Context, _ *mcp.CallToolRequest, in documentInput) (*mcp.CallToolResult, downloadOutput, error) {
	var empty downloadOutput
	org, err := readOrganization(ctx)
	if err != nil {
		return nil, empty, err
	}
	doc, err := d.document(ctx, org.ID, in.DocumentID)
	if err != nil {
		return nil, empty, err
	}
	if doc.Status != enums.DocumentStatusUploaded {
		return nil, empty, errors.New("Document file unavailable; processing may still be pending")
	}
	if d.store == nil || d.keys == nil {
		return nil, empty, errors.New("Document downloads are unavailable")
	}
	opts := &documentdownloads.CreateOptions{Resource: d.resource}
	if key := apikeys.FromContext(ctx); key != nil {
		opts.APIKeyID = key.ID
	} else if grant := oauth.FromContext(ctx); grant != nil {
		opts.OAuthAccessHash = grant.AccessHash
	}
	token, expires, err := documentdownloads.Create(ctx, d.db, org.ID, doc.ID, opts)
	if errors.Is(err, documentdownloads.ErrInvalidDownload) {
		return nil, empty, errors.New("Document access denied")
	}
	if err != nil {
		return nil, empty, documentError(ctx, err)
	}
	return nil, downloadOutput{
		DocumentID: doc.ExternalID, DownloadURL: d.resource + "/download?token=" + url.QueryEscape(token),
		ExpiresAt: expires, Filename: doc.Filename, ContentType: doc.ContentType, Size: doc.Size,
	}, nil
}

type downloadTokenKey struct{}

// Remove the capability before request logging, including for invalid requests.
func redactDownloadToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if path.Clean(r.URL.Path) == "/mcp/download" {
			token := ""
			if len(r.URL.RawQuery) <= 256 {
				values, err := url.ParseQuery(r.URL.RawQuery)
				if err == nil && len(values) == 1 && len(values["token"]) == 1 {
					token = values.Get("token")
				}
			}
			r = r.Clone(context.WithValue(r.Context(), downloadTokenKey{}, token))
			r.URL.RawQuery = ""
			r.RequestURI = r.URL.RequestURI()
		}
		next.ServeHTTP(w, r)
	})
}

func (d *documentTools) serveDownload(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	ctx := r.Context()
	token, _ := ctx.Value(downloadTokenKey{}).(string)
	download, err := documentdownloads.Resolve(ctx, d.db, token, d.resource)
	if errors.Is(err, documentdownloads.ErrInvalidDownload) || err == nil && download == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	org, err := organizations.Get(ctx, d.db, download.OrganizationID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if org == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	doc, err := documents.GetByExternalID(ctx, d.db, org.ID, download.DocumentID)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if doc == nil || doc.Status != enums.DocumentStatusUploaded {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	if d.store == nil || d.keys == nil {
		httputil.Error(ctx, w, errors.ErrInternalServerError)
		return
	}
	ctx = encrypt.WithContext(ctx, encrypt.ForOrganization(d.keys, org.ExternalID))
	plaintext, err := documentfiles.ReadContent(ctx, d.store, doc)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer clear(plaintext)
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": doc.Filename}))
	http.ServeContent(downloadWriter{w}, r, doc.Filename, time.Time{}, bytes.NewReader(plaintext))
}

type downloadWriter struct{ http.ResponseWriter }

func (w downloadWriter) WriteHeader(status int) {
	// ServeContent removes cache headers on range errors.
	w.Header().Set("Cache-Control", "no-store")
	w.ResponseWriter.WriteHeader(status)
}
