package documents

import (
	"mime"
	"net/http"
	"strconv"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/documentfiles"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
)

func (m *Mux) Content(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	ctx := r.Context()
	org, err := m.organizationAccess(r, enums.ScopeRead)
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
	if doc.PageCount > 0 && doc.PDFKey == "" {
		httputil.Error(ctx, w, errors.New("document PDF is unavailable"))
		return
	}
	if err := m.auditAccess(ctx, org.ID, doc.ExternalID, enums.AuditTypeDocumentContent); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	plaintext, err := documentfiles.ReadContent(ctx, m.store, doc)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer clear(plaintext)
	w.Header().Set("Content-Type", doc.ContentType)
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": doc.Filename}))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	_, _ = w.Write(plaintext)
}
