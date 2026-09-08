package documents

import (
	"io"
	"mime"
	"net/http"
	"strconv"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
)

func (m *Mux) Content(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	org, err := m.organizationAccess(r, apikeys.ScopeRead)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	enc := encrypt.FromContext(ctx)
	if !enc.IsOrganization(org.ExternalID) {
		httputil.Error(ctx, w, ErrForbidden)
		return
	}
	id, ok := mux.PathParam(r, "documentID")
	if !ok {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	doc, err := documents.GetByExternalID(ctx, m.db, org.ID, id)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if doc == nil || doc.Status != enums.DocumentStatusUploaded {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	if m.store == nil {
		httputil.Error(ctx, w, ErrStorageUnavailable)
		return
	}
	key := doc.ObjectKey
	if doc.PageCount > 0 {
		if doc.PDFKey == "" {
			httputil.Error(ctx, w, errors.New("document PDF is unavailable"))
			return
		}
		key = doc.PDFKey
	}
	if err := m.auditAccess(ctx, org.ID, doc.ExternalID, enums.AuditTypeDocumentContent); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	object, err := m.store.Get(ctx, key, nil)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer object.Body.Close()
	// Allow envelope overhead within the upload budget; Open enforces the plaintext limit.
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxUploadBody+1))
	if err != nil {
		httputil.Error(ctx, w, errors.Wrap(err, "unable to read encrypted document"))
		return
	}
	plaintext, err := enc.Open(ctx, ciphertext, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer clear(plaintext)
	if int64(len(plaintext)) != doc.Size {
		httputil.Error(ctx, w, errors.New("document size does not match content"))
		return
	}
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": doc.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	_, _ = w.Write(plaintext)
}
