package documents

import (
	"bytes"
	"context"
	"io"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/httputil"
	"nautilus/internal/log"
	"nautilus/internal/objectstore"
	"nautilus/internal/optional"
	"nautilus/internal/scan"
	"nautilus/internal/workflows/upload"
)

const maxUploadBody = encrypt.MaxPlaintextSize + 64<<10

func (m *Mux) Upload(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	w.Header().Set("Cache-Control", "no-store")
	if !m.admin || !sessionAdmin(ctx) {
		httputil.Error(ctx, w, ErrForbidden)
		return
	}
	org, err := m.organizationAccess(r, enums.ScopeWrite)
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
	defer form.clear()
	pages := make([]documents.PageOptions, len(form.Pages))
	for i, page := range form.Pages {
		pages[i] = documents.PageOptions{ContentType: page.ContentType, Size: int64(len(page.Data))}
	}
	doc, err := documents.Create(ctx, m.db, org.ID, &documents.CreateOptions{
		Filename: form.Filename, ContentType: "application/pdf", Pages: pages,
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
	if err := m.auditAccess(ctx, org.ID, doc.ExternalID, enums.AuditTypeDocumentUpload); err != nil {
		httputil.Error(ctx, w, err)
		return
	}
	for i, page := range form.Pages {
		number := strconv.Itoa(i + 1)
		ciphertext, err := enc.Seal(ctx, page.Data, encrypt.Binding{Purpose: "document-page", RecordID: doc.ExternalID + "/" + number})
		clear(page.Data)
		if err != nil {
			httputil.Error(ctx, w, err)
			return
		}
		if err := m.store.Put(ctx, doc.ObjectKey+"/pages/"+number, bytes.NewReader(ciphertext), &objectstore.PutOptions{
			ContentType: optional.Set("application/octet-stream"),
		}); err != nil {
			httputil.Error(ctx, w, err)
			return
		}
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
	Filename string       `json:"filename"`
	Pages    []uploadPage `json:"-"`
}

type uploadPage struct {
	Data        []byte
	ContentType string
}

func (f *uploadForm) clear() {
	for _, page := range f.Pages {
		clear(page.Data)
	}
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
	form := new(uploadForm)
	valid := false
	defer func() {
		if !valid {
			form.clear()
		}
	}()
	total := 0
	for {
		part, err := reader.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, multipartError(err)
		}
		if part.FormName() != "file" || part.FileName() == "" {
			return nil, ErrInvalidUpload
		}
		if len(form.Pages) == scan.MaxPages {
			return nil, ErrTooManyPages
		}
		name := uploadForm{Filename: part.FileName()}
		name.Normalize()
		if err := name.Validate(); err != nil {
			return nil, err
		}
		if len(form.Pages) == 0 {
			form.Filename = name.Filename
		}
		data, err := io.ReadAll(io.LimitReader(part, int64(encrypt.MaxPlaintextSize-total+1)))
		form.Pages = append(form.Pages, uploadPage{Data: data})
		if err != nil {
			return nil, multipartError(err)
		}
		total += len(data)
		if total > encrypt.MaxPlaintextSize {
			return nil, ErrUploadTooLarge
		}
	}
	if len(form.Pages) == 0 {
		return nil, ErrInvalidUpload
	}
	// Include any multipart epilogue in the request's total byte budget.
	if _, err := io.Copy(io.Discard, r.Body); err != nil {
		return nil, multipartError(err)
	}
	for i := range form.Pages {
		if err := r.Context().Err(); err != nil {
			return nil, errors.Wrap(err, "scan upload canceled")
		}
		contentType, err := scan.Validate(form.Pages[i].Data)
		if err != nil {
			return nil, ErrInvalidImage
		}
		form.Pages[i].ContentType = contentType
	}
	name := []rune(strings.TrimSuffix(form.Filename, path.Ext(form.Filename)))
	if len(name) == 0 {
		name = []rune("document")
	}
	form.Filename = string(name[:min(len(name), 251)]) + ".pdf"
	valid = true
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
