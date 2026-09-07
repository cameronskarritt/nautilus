package upload

import (
	"context"
	"io"

	"go.temporal.io/sdk/temporal"

	"nautilus/internal/crypto/encrypt"
	"nautilus/internal/database/documents"
	"nautilus/internal/database/organizations"
	"nautilus/internal/enums"
	"nautilus/internal/search"
)

// Index reads the encrypted OCR artifact so retries never need plaintext in history.
func (a Activities) Index(ctx context.Context, input Input) error {
	if err := input.normalize(); err != nil {
		return err
	}
	doc, err := documents.GetByExternalID(ctx, a.DB, input.OrganizationID, input.DocumentID)
	if err != nil {
		return indexFailure("unable to read document", false)
	}
	if doc == nil || doc.Status != enums.DocumentStatusUploaded {
		return indexFailure("document unavailable", true)
	}
	org, err := organizations.Get(ctx, a.DB, input.OrganizationID)
	if err != nil {
		return indexFailure("unable to read organization", false)
	}
	if org == nil {
		return indexFailure("organization unavailable", true)
	}
	object, err := a.Store.Get(ctx, doc.ObjectKey+"/ocr", nil)
	if err != nil {
		return indexFailure("unable to fetch document text", false)
	}
	defer object.Body.Close()
	const maxEnvelope = encrypt.MaxPlaintextSize + 64<<10
	ciphertext, err := io.ReadAll(io.LimitReader(object.Body, maxEnvelope+1))
	if err != nil {
		return indexFailure("unable to read document text", false)
	}
	if len(ciphertext) > maxEnvelope {
		return indexFailure("document text too large", true)
	}
	text, err := encrypt.ForOrganization(a.Keys, org.ExternalID).Open(ctx, ciphertext, encrypt.Binding{Purpose: "document-ocr", RecordID: doc.ExternalID})
	if err != nil {
		return indexFailure("unable to decrypt document text", false)
	}
	defer clear(text)
	// Recheck deletion after object storage and KMS I/O. Search consumers must still
	// authorize hits against PostgreSQL because cross-store writes are not atomic.
	org, err = organizations.Get(ctx, a.DB, input.OrganizationID)
	if err != nil {
		return indexFailure("unable to read organization", false)
	}
	if org == nil {
		return indexFailure("organization unavailable", true)
	}
	if err := a.Indexer.Index(ctx, org.ExternalID, &search.Document{ID: doc.ExternalID, Text: doc.Filename + "\n" + string(text)}); err != nil {
		return indexFailure("unable to index document", false)
	}
	return nil
}

func indexFailure(message string, terminal bool) error {
	if terminal {
		return temporal.NewNonRetryableApplicationError(message, "IndexUnavailable", nil) //nolint:wrapcheck // Preserve Temporal failure semantics without a sensitive cause.
	}
	return temporal.NewApplicationError(message, "IndexFailed") //nolint:wrapcheck // Provider errors must not enter Temporal history.
}
