package upload

import (
	"context"
	"io"
	"strconv"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/scan"
	"nautilus/internal/temporal/failure"
)

const maxSourceBytes = 100 << 20

// sourcePages authenticates every page's position before returning any image.
func (a Activities) sourcePages(ctx context.Context, doc *documents.Document, enc *encrypt.Encrypter) ([][]byte, []documents.Page, error) {
	pages, err := documents.ListPages(ctx, a.DB, doc.OrganizationID, doc.ExternalID)
	if err != nil {
		return nil, nil, uploadFailure("unable to read document pages", false)
	}
	if doc.PageCount <= 0 || doc.PageCount > scan.MaxPages || len(pages) != doc.PageCount {
		return nil, nil, uploadFailure("document page count mismatch", true)
	}
	var total int64
	for i, page := range pages {
		if page.Number != i+1 || page.Size <= 0 || page.Size > maxSourceBytes-total || page.ObjectKey == "" {
			return nil, nil, uploadFailure("invalid document page metadata", true)
		}
		total += page.Size
	}
	images := make([][]byte, 0, len(pages))
	complete := false
	defer func() {
		if !complete {
			for _, data := range images {
				clear(data)
			}
		}
	}()
	for _, page := range pages {
		if ctx.Err() != nil {
			return nil, nil, uploadFailure("document processing canceled", false)
		}
		data, err := a.readPage(ctx, doc, page, enc)
		if err != nil {
			return nil, nil, err
		}
		images = append(images, data)
	}
	complete = true
	return images, pages, nil
}

func (a Activities) readPage(ctx context.Context, doc *documents.Document, page documents.Page, enc *encrypt.Encrypter) ([]byte, error) {
	object, err := a.Store.Get(ctx, page.ObjectKey, nil)
	if err != nil {
		return nil, uploadFailure("unable to fetch document page", false)
	}
	defer object.Body.Close()
	limit := page.Size + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, limit+1))
	if err != nil {
		return nil, uploadFailure("unable to read document page", false)
	}
	if int64(len(ciphertext)) > limit {
		return nil, uploadFailure("document page too large", true)
	}
	data, err := enc.Open(ctx, ciphertext, encrypt.Binding{Purpose: "document-page", RecordID: doc.ExternalID + "/" + strconv.Itoa(page.Number)})
	if err != nil {
		return nil, uploadFailure("unable to decrypt document page", false)
	}
	if int64(len(data)) != page.Size {
		clear(data)
		return nil, uploadFailure("document page size mismatch", true)
	}
	contentType, err := scan.Validate(data)
	if err != nil || contentType != page.ContentType {
		clear(data)
		return nil, uploadFailure("invalid document page image", true)
	}
	return data, nil
}

func uploadFailure(message string, terminal bool) error {
	if terminal {
		return failure.New(message, "UploadUnavailable", true)
	}
	return failure.New(message, "UploadFailed", false)
}
