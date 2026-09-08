package upload

import (
	"bytes"
	"context"
	"io"
	"strings"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/errors"
	"nautilus/internal/objectstore"
	"nautilus/internal/ocr"
	"nautilus/internal/optional"
)

// Extract keeps document plaintext and OCR output inside the activity. Retries
// replace the same encrypted artifact; the original document remains uploaded.
func (a Activities) Extract(ctx context.Context, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	ctx, stop := heartbeat(ctx)
	defer stop()
	doc, err := documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
	if err != nil {
		return ocrFailure("unable to read document", false)
	}
	if doc == nil || doc.Status != enums.DocumentStatusUploaded {
		return ocrFailure("document unavailable", true)
	}
	org, err := organizations.Get(ctx, a.DB, input.OrganizationID)
	if err != nil {
		return ocrFailure("unable to read organization", false)
	}
	if org == nil {
		return ocrFailure("organization unavailable", true)
	}
	enc := encrypt.ForOrganization(a.Keys, org.ExternalID)
	var text string
	if doc.PageCount > 0 {
		text, err = a.extractPages(ctx, doc, enc)
	} else {
		text, err = a.extractOriginal(ctx, doc, enc)
	}
	if err != nil {
		return err
	}
	if len(text) > encrypt.MaxPlaintextSize {
		return ocrFailure("document text too large", true)
	}
	output := []byte(text)
	defer clear(output)
	encrypted, err := enc.Seal(ctx, output, encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	if err != nil {
		return ocrFailure("unable to encrypt document text", false)
	}
	if err := a.Store.Put(ctx, doc.ObjectKey+"/ocr", bytes.NewReader(encrypted), &objectstore.PutOptions{ContentType: optional.Set("application/octet-stream")}); err != nil {
		return ocrFailure("unable to store document text", false)
	}
	return nil
}

func ocrFailure(message string, terminal bool) error {
	// Never attach provider errors, which can contain plaintext, to Temporal history.
	if terminal {
		return temporal.NewNonRetryableApplicationError(message, "OCRUnavailable", nil) //nolint:wrapcheck // Preserve Temporal nonretryable semantics without a sensitive cause.
	}
	return temporal.NewApplicationError(message, "OCRFailed") //nolint:wrapcheck // Only sanitized errors may enter Temporal history.
}

func (a Activities) extractOriginal(ctx context.Context, doc *documents.Document, enc *encrypt.Encrypter) (string, error) {
	object, err := a.Store.Get(ctx, doc.ObjectKey, nil)
	if err != nil {
		return "", ocrFailure("unable to fetch document", false)
	}
	defer object.Body.Close()
	const maxEnvelope = encrypt.MaxPlaintextSize + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxEnvelope+1))
	if err != nil {
		return "", ocrFailure("unable to read document content", false)
	}
	if len(ciphertext) > maxEnvelope {
		return "", ocrFailure("document content too large", true)
	}
	plaintext, err := enc.Open(ctx, ciphertext, encrypt.Binding{Purpose: "document", RecordID: doc.ExternalID})
	if err != nil {
		return "", ocrFailure("unable to decrypt document", false)
	}
	defer clear(plaintext)
	if int64(len(plaintext)) != doc.Size {
		return "", ocrFailure("document size mismatch", true)
	}
	text, err := a.OCR.Extract(ctx, bytes.NewReader(plaintext), doc.ContentType)
	if err != nil {
		return "", ocrFailure("unable to extract document text", errors.Is(err, ocr.ErrInvalidDocument))
	}
	return text, nil
}

func (a Activities) extractPages(ctx context.Context, doc *documents.Document, enc *encrypt.Encrypter) (string, error) {
	images, pages, err := a.sourcePages(ctx, doc, enc)
	if err != nil {
		return "", err
	}
	defer func() {
		for _, data := range images {
			clear(data)
		}
	}()
	var text strings.Builder
	for i, data := range images {
		if ctx.Err() != nil {
			return "", ocrFailure("document processing canceled", false)
		}
		page, err := a.OCR.Extract(ctx, bytes.NewReader(data), pages[i].ContentType)
		clear(data)
		if err != nil {
			return "", ocrFailure("unable to extract document text", errors.Is(err, ocr.ErrInvalidDocument))
		}
		separator := 0
		if i > 0 {
			separator = 1
		}
		if len(page) > encrypt.MaxPlaintextSize-text.Len()-separator {
			return "", ocrFailure("document text too large", true)
		}
		if i > 0 {
			text.WriteByte('\n')
		}
		text.WriteString(page)
	}
	return text.String(), nil
}
