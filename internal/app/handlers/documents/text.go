package documents

import (
	"io"
	"net/http"
	"strconv"
	"unicode/utf8"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/mux"
	"nautilus/internal/objectstore"
)

func (m *Mux) Text(w http.ResponseWriter, r *http.Request) {
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
	if doc == nil {
		httputil.Error(ctx, w, errors.ErrNotFound)
		return
	}
	if doc.Status != enums.DocumentStatusUploaded {
		httputil.Error(ctx, w, ErrTextUnavailable)
		return
	}
	if m.store == nil {
		httputil.Error(ctx, w, ErrReadStorageUnavailable)
		return
	}
	if err := m.auditAccess(ctx, org.ID, doc.ExternalID, enums.AuditTypeDocumentText); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	object, err := m.store.Get(ctx, doc.ObjectKey+"/ocr", nil)
	if errors.Is(err, objectstore.ErrNotFound) {
		httputil.Error(ctx, w, ErrTextUnavailable)
		return
	}
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer object.Body.Close()
	const maxEnvelope = encrypt.MaxPlaintextSize + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxEnvelope+1))
	if err != nil {
		httputil.Error(ctx, w, errors.Wrap(err, "unable to read encrypted document text"))
		return
	}
	if len(ciphertext) > maxEnvelope {
		httputil.Error(ctx, w, errors.New("encrypted document text exceeds size limit"))
		return
	}
	plaintext, err := enc.Open(ctx, ciphertext, encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer clear(plaintext)
	if !utf8.Valid(plaintext) {
		httputil.Error(ctx, w, errors.New("document text is not valid UTF-8"))
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Length", strconv.Itoa(len(plaintext)))
	_, _ = w.Write(plaintext)
}
