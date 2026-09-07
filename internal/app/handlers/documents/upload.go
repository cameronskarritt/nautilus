package documents

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"path"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/apikeys"
	"nautilus/internal/database/documents"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/workflows/upload"
)

const maxUploadBody = encrypt.MaxPlaintextSize + 64<<10

func (m *Mux) Upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	org, err := organizationAccess(r, apikeys.ScopeWrite)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	enc := encrypt.FromContext(ctx)
	if !enc.IsOrganization(org.ExternalID) {
		httputil.Error(ctx, w, ErrForbidden)
		return
	}
	if m.store == nil {
		httputil.Error(ctx, w, ErrStorageUnavailable)
		return
	}
	if m.workflows == nil {
		httputil.Error(ctx, w, ErrWorkflowUnavailable)
		return
	}
	form, err := readUpload(w, r)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	defer clear(form.Data)
	contentType := "application/octet-stream"
	if len(form.Data) != 0 {
		contentType = http.DetectContentType(form.Data)
	}
	doc, err := documents.Create(ctx, m.db, org.ID, &documents.CreateOptions{
		Filename: form.Filename, ContentType: contentType, Size: int64(len(form.Data)),
	})
	if err != nil {
		httputil.Error(ctx, w, uploadError(err))
		return
	}
	if doc == nil {
		httputil.Error(ctx, w, ErrForbidden)
		return
	}
	uploaded := false
	defer func() {
		if uploaded {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		if err := documents.MarkFailed(cleanup, m.db, org.ID, doc.ExternalID); err != nil {
			log.FromContext(ctx).Error("unable to mark document upload failed", "document", doc.ExternalID, "error", err)
		}
	}()
	ciphertext, err := enc.Seal(ctx, form.Data, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	clear(form.Data)
	if err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	if err := m.store.Put(ctx, doc.ObjectKey, bytes.NewReader(ciphertext), &objectstore.PutOptions{
		ContentType: optional.Set("application/octet-stream"),
	}); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	// A failed start response may still represent an accepted workflow.
	uploaded = true
	if err := upload.Start(ctx, m.workflows, upload.Input{OrganizationID: org.ID, DocumentID: doc.ExternalID}); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	httputil.JSON(ctx, w, httputil.Map{"document": doc}, http.StatusAccepted)
}

type uploadForm struct {
	Filename string `json:"filename"`
	Data     []byte `json:"-"`
}

func (f *uploadForm) Normalize() {
	f.Filename = strings.TrimSpace(path.Base(strings.ReplaceAll(strings.TrimSpace(f.Filename), "\\", "/")))
}

func (f *uploadForm) Validate() error {
	if f.Filename == "" || f.Filename == "." || f.Filename == ".." || f.Filename == "/" ||
		!utf8.ValidString(f.Filename) || utf8.RuneCountInString(f.Filename) > 255 || strings.ContainsFunc(f.Filename, unicode.IsControl) {
		return ErrInvalidFilename
	}
	return nil
}

// MultipartReader streams the file into bounded memory without creating plaintext temporary files.
func readUpload(w http.ResponseWriter, r *http.Request) (*uploadForm, error) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, ErrUnsupportedUpload
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBody)
	reader, err := r.MultipartReader()
	if err != nil {
		return nil, ErrInvalidUpload
	}
	part, err := reader.NextPart()
	if err != nil {
		return nil, multipartError(err)
	}
	if part.FormName() != "file" || part.FileName() == "" {
		return nil, ErrInvalidUpload
	}
	form := &uploadForm{Filename: part.FileName()}
	form.Normalize()
	if err := form.Validate(); err != nil {
		return nil, err
	}
	form.Data, err = io.ReadAll(io.LimitReader(part, encrypt.MaxPlaintextSize+1))
	if err != nil {
		clear(form.Data)
		return nil, multipartError(err)
	}
	if len(form.Data) > encrypt.MaxPlaintextSize {
		clear(form.Data)
		return nil, ErrUploadTooLarge
	}
	if _, err := reader.NextPart(); err != io.EOF {
		clear(form.Data)
		return nil, multipartError(err)
	}
	// Include any multipart epilogue in the request's total byte budget.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		clear(form.Data)
		return nil, multipartError(err)
	}
	return form, nil
}

func multipartError(err error) error {
	var maxBytes *http.MaxBytesError
	if errors.As(err, &maxBytes) {
		return ErrUploadTooLarge
	}
	return ErrInvalidUpload
}

func uploadError(err error) error {
	switch {
	case errors.Is(err, documents.ErrInvalidOrganization):
		return ErrForbidden
	case errors.Is(err, documents.ErrInvalidFilename):
		return ErrInvalidFilename
	case errors.Is(err, documents.ErrInvalidContentType), errors.Is(err, documents.ErrInvalidSize):
		return ErrInvalidUpload
	default:
		return err
	}
}
